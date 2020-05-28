// Copyright 2020 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tcp_queue_receive_in_syn_sent_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	tb "gvisor.dev/gvisor/test/packetimpact/testbench"
)

func init() {
	tb.RegisterFlags(flag.CommandLine)
}

func TestQueueReceiveInSynSent(t *testing.T) {
	dut := tb.NewDUT(t)
	defer dut.TearDown()

	socket, remotePort := dut.CreateBoundSocket(unix.SOCK_STREAM, unix.IPPROTO_TCP, net.ParseIP(tb.RemoteIPv4))
	localIP := net.ParseIP(tb.LocalIPv4)
	localPort := uint16(1234)

	conn := tb.NewTCPIPv4(t, tb.TCP{SrcPort: &localPort, DstPort: &remotePort}, tb.TCP{SrcPort: &remotePort, DstPort: &localPort})
	defer conn.Close()

	sampleData := []byte("Sample Data")

	// Bring DUT to SYN-SENT.
	sa := unix.SockaddrInet4{}
	copy(sa.Addr[:], localIP.To4())
	sa.Port = int(localPort)
	dut.SetNonBlocking(socket, true)
	if _, err := dut.ConnectWithErrno(context.Background(), socket, &sa); !errors.Is(err, syscall.EINPROGRESS) {
		t.Fatalf("failed to bring DUT to SYN-SENT, want EINPROGRESS, got: %s", err)
	}
	if _, err := conn.Expect(tb.TCP{Flags: tb.Uint8(header.TCPFlagSyn)}, time.Second); err != nil {
		t.Fatalf("expected a SYN from DUT, but got none: %s", err)
	}

	// Issue RECEIVE call in SYN-SENT, this should be queued for process until the connection
	// is established.
	dut.SetNonBlocking(socket, false)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		n, buff, err := dut.RecvWithErrno(ctx, socket, int32(len(sampleData)), 0)
		if n == -1 {
			t.Fatalf("failed to recv on DUT: %s", err)
		}
		if !bytes.Equal(buff[:n], sampleData) {
			t.Fatalf("received data don't match, want:\n%s, got:\n%s", hex.Dump(sampleData), hex.Dump(buff[:n]))
		}
	}()

	// The following sleep is used to prevent the connection from being established while the
	// RPC is in flight.
	time.Sleep(time.Second)

	// Bring the connection to Established.
	conn.Send(tb.TCP{Flags: tb.Uint8(header.TCPFlagSyn | header.TCPFlagAck)})
	if _, err := conn.Expect(tb.TCP{Flags: tb.Uint8(header.TCPFlagAck)}, time.Second); err != nil {
		t.Fatalf("expected an ACK from DUT, but got none: %s", err)
	}

	// Send sample data to DUT.
	conn.Send(tb.TCP{Flags: tb.Uint8(header.TCPFlagAck)}, &tb.Payload{Bytes: sampleData})
	wg.Wait()
}
