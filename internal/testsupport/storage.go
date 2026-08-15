// Package testsupport contains deterministic synthetic fixtures used only by tests.
package testsupport

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/SalehElnagar/dar-download-app/internal/blob"
)

// Object is one synthetic immutable Blob representation.
type Object struct {
	Data []byte
	ETag string
}

// OpenCall records one exact payload segment request.
type OpenCall struct {
	BlobName string
	Offset   int64
	Length   int64
	ETag     string
}

// Storage is a concurrency-safe Blob test double.
type Storage struct {
	mu sync.Mutex

	Objects  map[string]Object
	StatErr  error
	OpenErr  error
	OpenHook func(context.Context, OpenCall) (io.ReadCloser, error)

	StatCalls []string
	OpenCalls []OpenCall
	active    int
	maxActive int
}

// Stat implements blob.Store.
func (storage *Storage) Stat(_ context.Context, blobName string) (blob.Snapshot, error) {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.StatCalls = append(storage.StatCalls, blobName)
	if storage.StatErr != nil {
		return blob.Snapshot{}, storage.StatErr
	}
	object, ok := storage.Objects[blobName]
	if !ok {
		return blob.Snapshot{}, blob.ErrNotFound
	}
	return blob.Snapshot{Size: int64(len(object.Data)), ETag: object.ETag}, nil
}

// OpenRange implements blob.Store.
func (storage *Storage) OpenRange(
	ctx context.Context,
	blobName string,
	offset int64,
	length int64,
	etag string,
) (io.ReadCloser, error) {
	call := OpenCall{BlobName: blobName, Offset: offset, Length: length, ETag: etag}
	storage.mu.Lock()
	storage.OpenCalls = append(storage.OpenCalls, call)
	if storage.OpenErr != nil {
		err := storage.OpenErr
		storage.mu.Unlock()
		return nil, err
	}
	hook := storage.OpenHook
	object, ok := storage.Objects[blobName]
	if !ok {
		storage.mu.Unlock()
		return nil, blob.ErrNotFound
	}
	if object.ETag != etag {
		storage.mu.Unlock()
		return nil, blob.ErrChanged
	}
	if offset < 0 || length < 0 || offset+length > int64(len(object.Data)) {
		storage.mu.Unlock()
		return nil, blob.ErrUnavailable
	}
	storage.active++
	if storage.active > storage.maxActive {
		storage.maxActive = storage.active
	}
	storage.mu.Unlock()

	if hook != nil {
		reader, err := hook(ctx, call)
		if err != nil {
			storage.readerClosed()
			return nil, err
		}
		return &trackedReader{ReadCloser: reader, onClose: storage.readerClosed}, nil
	}
	data := object.Data[offset : offset+length]
	return &trackedReader{
		ReadCloser: io.NopCloser(bytes.NewReader(data)),
		onClose:    storage.readerClosed,
	}, nil
}

// Counts returns stable copies of the observed calls and maximum active readers.
func (storage *Storage) Counts() ([]string, []OpenCall, int) {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	return append([]string(nil), storage.StatCalls...),
		append([]OpenCall(nil), storage.OpenCalls...), storage.maxActive
}

func (storage *Storage) readerClosed() {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.active--
}

type trackedReader struct {
	io.ReadCloser
	once    sync.Once
	onClose func()
}

func (reader *trackedReader) Close() error {
	err := reader.ReadCloser.Close()
	reader.once.Do(reader.onClose)
	return err
}
