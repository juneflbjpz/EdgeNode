package caches

import (
	"encoding/binary"
	"errors"
	"github.com/iwind/TeaGo/types"
	"io"
	"os"
	"strings"
	"sync"
)

type FileWriter struct {
	storage   StorageInterface
	rawWriter *os.File
	key       string

	metaHeaderSize int
	headerSize     int64

	metaBodySize int64 // 写入前的内容长度
	bodySize     int64

	expiredAt int64
	maxSize   int64
	endFunc   func()
	once      sync.Once
}

func NewFileWriter(storage StorageInterface, rawWriter *os.File, key string, expiredAt int64, metaHeaderSize int, metaBodySize int64, maxSize int64, endFunc func()) *FileWriter {
	return &FileWriter{
		storage:        storage,
		key:            key,
		rawWriter:      rawWriter,
		expiredAt:      expiredAt,
		maxSize:        maxSize,
		endFunc:        endFunc,
		metaHeaderSize: metaHeaderSize,
		metaBodySize:   metaBodySize,
	}
}

// WriteHeader 写入数据
func (this *FileWriter) WriteHeader(data []byte) (n int, err error) {
	n, err = this.rawWriter.Write(data)
	this.headerSize += int64(n)
	if err != nil {
		_ = this.Discard()
	}
	return
}

// WriteHeaderLength 写入Header长度数据
func (this *FileWriter) WriteHeaderLength(headerLength int) error {
	if this.metaHeaderSize > 0 && this.metaHeaderSize == headerLength {
		return nil
	}
	var bytes4 = make([]byte, 4)
	binary.BigEndian.PutUint32(bytes4, uint32(headerLength))
	_, err := this.rawWriter.Seek(SizeExpiresAt+SizeStatus+SizeURLLength, io.SeekStart)
	if err != nil {
		_ = this.Discard()
		return err
	}
	_, err = this.rawWriter.Write(bytes4)
	if err != nil {
		_ = this.Discard()
		return err
	}
	return nil
}

// Write 写入数据
func (this *FileWriter) Write(data []byte) (n int, err error) {
	n, err = this.rawWriter.Write(data)
	this.bodySize += int64(n)

	if this.maxSize > 0 && this.bodySize > this.maxSize {
		err = ErrEntityTooLarge

		if this.storage != nil {
			this.storage.IgnoreKey(this.key)
		}
	}

	if err != nil {
		_ = this.Discard()
	}
	return
}

// WriteAt 在指定位置写入数据
func (this *FileWriter) WriteAt(offset int64, data []byte) error {
	_ = data
	_ = offset
	return errors.New("not supported")
}

// WriteBodyLength 写入Body长度数据
func (this *FileWriter) WriteBodyLength(bodyLength int64) error {
	if this.metaBodySize >= 0 && bodyLength == this.metaBodySize {
		return nil
	}
	var bytes8 = make([]byte, 8)
	binary.BigEndian.PutUint64(bytes8, uint64(bodyLength))
	_, err := this.rawWriter.Seek(SizeExpiresAt+SizeStatus+SizeURLLength+SizeHeaderLength, io.SeekStart)
	if err != nil {
		_ = this.Discard()
		return err
	}
	_, err = this.rawWriter.Write(bytes8)
	if err != nil {
		_ = this.Discard()
		return err
	}
	return nil
}

// Close 关闭
func (this *FileWriter) Close() error {
	defer this.once.Do(func() {
		this.endFunc()
	})

	var path = this.rawWriter.Name()

	err := this.WriteHeaderLength(types.Int(this.headerSize))
	if err != nil {
		_ = this.rawWriter.Close()
		_ = os.Remove(path)
		return err
	}
	err = this.WriteBodyLength(this.bodySize)
	if err != nil {
		_ = this.rawWriter.Close()
		_ = os.Remove(path)
		return err
	}

	err = this.rawWriter.Close()
	if err != nil {
		_ = os.Remove(path)
	} else if strings.HasSuffix(path, FileTmpSuffix) {
		err = os.Rename(path, strings.Replace(path, FileTmpSuffix, "", 1))
		if err != nil {
			_ = os.Remove(path)
		}
	}

	return err
}

// Discard 丢弃
func (this *FileWriter) Discard() error {
	defer this.once.Do(func() {
		this.endFunc()
	})

	_ = this.rawWriter.Close()

	err := os.Remove(this.rawWriter.Name())
	return err
}

func (this *FileWriter) HeaderSize() int64 {
	return this.headerSize
}

func (this *FileWriter) BodySize() int64 {
	return this.bodySize
}

func (this *FileWriter) ExpiredAt() int64 {
	return this.expiredAt
}

func (this *FileWriter) Key() string {
	return this.key
}

// ItemType 获取内容类型
func (this *FileWriter) ItemType() ItemType {
	return ItemTypeFile
}
