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

package vfs2

import (
	"gvisor.dev/gvisor/pkg/abi/linux"
	"gvisor.dev/gvisor/pkg/sentry/arch"
	"gvisor.dev/gvisor/pkg/sentry/kernel"
	"gvisor.dev/gvisor/pkg/sentry/vfs"
	"gvisor.dev/gvisor/pkg/syserror"
)

const allFlags = uint32(linux.IN_NONBLOCK | linux.IN_CLOEXEC)

// InotifyInit1 implements the inotify_init1() syscalls.
func InotifyInit1(t *kernel.Task, args arch.SyscallArguments) (uintptr, *kernel.SyscallControl, error) {
	flags := args[0].Uint()
	if flags&^allFlags != 0 {
		return 0, nil, syserror.EINVAL
	}

	ino, err := vfs.NewInotifyFD(t, t.Kernel().VFS(), flags)
	if err != nil {
		return 0, nil, err
	}
	defer ino.DecRef()

	fd, err := t.NewFDFromVFS2(0, ino, kernel.FDFlags{
		CloseOnExec: flags&linux.IN_CLOEXEC != 0,
	})

	if err != nil {
		return 0, nil, err
	}

	return uintptr(fd), nil, nil
}

// InotifyInit implements the inotify_init() syscalls.
func InotifyInit(t *kernel.Task, args arch.SyscallArguments) (uintptr, *kernel.SyscallControl, error) {
	args[0].Value = 0
	return InotifyInit1(t, args)
}

// fdToInotify resolves an fd to an inotify object. If successful, the file will
// have an extra ref and the caller is responsible for releasing the ref.
func fdToInotify(t *kernel.Task, fd int32) (*vfs.Inotify, *vfs.FileDescription, error) {
	f := t.GetFileVFS2(fd)
	if f == nil {
		// Invalid fd.
		return nil, nil, syserror.EBADF
	}

	ino, ok := f.Impl().(*vfs.Inotify)
	if !ok {
		// Not an inotify fd.
		f.DecRef()
		return nil, nil, syserror.EINVAL
	}

	return ino, f, nil
}

// InotifyAddWatch implements the inotify_add_watch() syscall.
func InotifyAddWatch(t *kernel.Task, args arch.SyscallArguments) (uintptr, *kernel.SyscallControl, error) {
	fd := args[0].Int()
	addr := args[1].Pointer()
	mask := args[2].Uint()

	// "EINVAL: The given event mask contains no valid events."
	// -- inotify_add_watch(2)
	if validBits := mask & linux.ALL_INOTIFY_BITS; validBits == 0 {
		return 0, nil, syserror.EINVAL
	}

	// "IN_DONT_FOLLOW: Don't dereference pathname if it is a symbolic link."
	//  -- inotify(7)
	follow := followFinalSymlink
	if mask&linux.IN_DONT_FOLLOW == 0 {
		follow = nofollowFinalSymlink
	}

	ino, f, err := fdToInotify(t, fd)
	if err != nil {
		return 0, nil, err
	}
	defer f.DecRef()

	path, err := copyInPath(t, addr)
	if err != nil {
		return 0, nil, err
	}
	if mask&linux.IN_ONLYDIR != 0 {
		path.Dir = true
	}
	tpop, err := getTaskPathOperation(t, linux.AT_FDCWD, path, disallowEmptyPath, follow)
	if err != nil {
		return 0, nil, err
	}
	defer tpop.Release()
	d, err := t.Kernel().VFS().GetDentryAt(t, t.Credentials(), &tpop.pop, &vfs.GetDentryOptions{})
	if err != nil {
		return 0, nil, err
	}
	defer d.DecRef()

	fd = ino.AddWatch(d.Dentry(), mask)
	return uintptr(fd), nil, err
}

// InotifyRmWatch implements the inotify_rm_watch() syscall.
func InotifyRmWatch(t *kernel.Task, args arch.SyscallArguments) (uintptr, *kernel.SyscallControl, error) {
	fd := args[0].Int()
	wd := args[1].Int()

	ino, f, err := fdToInotify(t, fd)
	if err != nil {
		return 0, nil, err
	}
	defer f.DecRef()
	return 0, nil, ino.RmWatch(wd)
}
