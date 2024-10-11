package pcstats

/*
 * Copyright 2014-2017 A. Tobey <tobert@gmail.com> @AlTobey
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// page cache status
// Bytes: size of the file (from os.File.Stat())
// Pages: array of booleans: true if cached, false otherwise
type PcStatus struct {
	Name      string    `json:"filename"`  // file name as specified on command line
	Size      int64     `json:"size"`      // file size in bytes
	Timestamp time.Time `json:"timestamp"` // time right before calling mincore
	Mtime     time.Time `json:"mtime"`     // last modification time of the file
	Pages     int       `json:"pages"`     // total memory pages
	Cached    int       `json:"cached"`    // number of pages that are cached
	Uncached  int       `json:"uncached"`  // number of pages that are not cached
	Percent   float64   `json:"percent"`   // percentage of pages cached
}

const BLKGETSIZE64 = 0x80081272

func _isBlockDevice(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	mode := info.Mode()
	return mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0, nil
}

func getBlockDeviceSize(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var size int64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, file.Fd(), BLKGETSIZE64, uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, errno
	}

	return size, nil
}

func GetPcStatus(fname string, filter func(f *os.File) error) (PcStatus, error) {
	pcs := PcStatus{Name: fname}

	f, err := os.Open(fname)
	if err != nil {
		return pcs, fmt.Errorf("could not open file for read: %v", err)
	}
	defer f.Close()

	if err := filter(f); err != nil {
		return pcs, err
	}

	// TEST TODO: verify behavior when the file size is changing quickly
	// while this function is running. I assume that the size parameter to
	// mincore will prevent overruns of the output vector, but it's not clear
	// what will be in there when the file is truncated between here and the
	// mincore() call.
	finfo, err := f.Stat()
	if err != nil {
		return pcs, fmt.Errorf("could not stat file: %v", err)
	}
	if finfo.IsDir() {
		return pcs, errors.New("file is a directory")
	}
	isBlock, err := _isBlockDevice(fname)
	if err != nil {
		return pcs, err
	}
	if isBlock {
		pcs.Size, err = getBlockDeviceSize(fname)
		if err != nil {
			return pcs, err
		}
	} else {
		pcs.Size = finfo.Size()
	}
	pcs.Timestamp = time.Now()
	pcs.Mtime = finfo.ModTime()

	mincore, err := GetFileMincore(f, pcs.Size)
	if err != nil {
		return pcs, err
	}
	if mincore == nil {
		return pcs, nil
	}

	pcs.Cached = int(mincore.Cached)
	pcs.Pages = int(mincore.Cached) + int(mincore.Miss)
	pcs.Uncached = int(mincore.Miss)

	pcs.Percent = (float64(pcs.Cached) / float64(pcs.Pages)) * 100.00
	return pcs, nil
}
