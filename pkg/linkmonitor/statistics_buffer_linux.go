package linkmonitor

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"golang.org/x/sys/unix"
)

const statisticNameBytes = 32
const maximumStatisticBuffer = 12 + maximumSamples*statisticNameBytes

// statisticBuffer ends every request immediately before an inaccessible page.
// Old ioctl implementations can ignore the caller's count; copy_to_user must
// fault instead of writing into another Go object when a schema grows.
type statisticBuffer struct{ mapping []byte }

func (b *statisticBuffer) bytes(size int) ([]byte, error) {
	if size < 4 || size > maximumStatisticBuffer {
		return nil, linuxio.ErrRequest
	}
	page := unix.Getpagesize()
	if b.mapping == nil {
		writable := (maximumStatisticBuffer + page - 1) / page * page
		memory, err := unix.Mmap(-1, 0, writable+page, unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANON)
		if err != nil {
			return nil, err
		}
		if err := unix.Mprotect(memory[:writable], unix.PROT_READ|unix.PROT_WRITE); err != nil {
			return nil, errors.Join(err, unix.Munmap(memory))
		}
		b.mapping = memory
	}
	end := len(b.mapping) - page
	data := b.mapping[end-size : end : end]
	clear(data)
	return data, nil
}

func (b *statisticBuffer) Close() error {
	if b.mapping == nil {
		return nil
	}
	err := unix.Munmap(b.mapping)
	b.mapping = nil
	return err
}
