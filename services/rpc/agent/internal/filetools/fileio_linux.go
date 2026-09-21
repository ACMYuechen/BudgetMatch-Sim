//go:build linux

package filetools

import (
	"fmt"
	"os"
	"syscall"
)

const secureFileIOSupported = true

func openRegularFile(root *os.Root, name string) (*os.File, error) {
	// O_NONBLOCK 避免 FIFO 在 Stat 前挂住请求；O_NOFOLLOW 拒绝最终文件的符号链接。
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		file.Close()
		return nil, fmt.Errorf("only single-link regular files are readable: %w", os.ErrPermission)
	}
	return file, nil
}
