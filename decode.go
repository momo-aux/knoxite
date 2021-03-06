/*
 * knoxite
 *     Copyright (c) 2016, Christian Muehlhaeuser <muesli@gmail.com>
 *
 *   For license see LICENSE.txt
 */

package knoxite

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/reedsolomon"
)

// ChunkError records an error and the index
// that caused it.
type ChunkError struct {
	ChunkNum uint
}

func (e *ChunkError) Error() string {
	return fmt.Sprintf("Could not find chunk #%d", e.ChunkNum)
}

// SeekError records an error and the offset
// that caused it.
type SeekError struct {
	Offset int
}

func (e *SeekError) Error() string {
	return fmt.Sprintf("Could not seek to offset %d", e.Offset)
}

// CheckSumError records an error and the calculated
// checksums that did not match.
type CheckSumError struct {
	Method           string
	ExpectedCheckSum string
	FoundCheckSum    string
}

func (e *CheckSumError) Error() string {
	return fmt.Sprintf("%s mismatch, expected %s, got %s", e.Method, e.ExpectedCheckSum, e.FoundCheckSum)
}

// DataReconstructionError records an error and the associated
// parity information
type DataReconstructionError struct {
	Chunk          Chunk
	BlocksFound    uint
	FailedBackends uint
}

func (e *DataReconstructionError) Error() string {
	return fmt.Sprintf("Could not reconstruct data, got %d out of %d chunks (%d backends missing data)", e.BlocksFound, e.Chunk.DataParts, e.FailedBackends)
}

// DecodeSnapshot restores an entire snapshot to dst
func DecodeSnapshot(repository Repository, snapshot Snapshot, dst string) (prog chan Progress, err error) {
	prog = make(chan Progress)
	go func() {
		for _, arc := range snapshot.Items {
			path := filepath.Join(dst, arc.Path)
			err := DecodeArchive(prog, repository, arc, path)
			if err != nil {
				panic(err)
			}
		}
		close(prog)
	}()

	return prog, nil
}

func decodeChunk(repository Repository, chunk Chunk, finalData []byte) ([]byte, error) {
	if chunk.Encrypted == EncryptionAES {
		data, err := Decrypt(finalData, repository.Password)
		if err != nil {
			return []byte{}, err
		}

		finalData = data
	}

	if chunk.Compressed == CompressionGZip {
		reader := bytes.NewReader(finalData)
		zipreader, err := gzip.NewReader(reader)
		if err != nil {
			return []byte{}, err
		}
		defer zipreader.Close()
		finalData, err = ioutil.ReadAll(zipreader)
		if err != nil {
			return []byte{}, err
		}
	}

	shasumdata := sha256.Sum256(finalData)
	shasum := hex.EncodeToString(shasumdata[:])

	if chunk.DecryptedShaSum != shasum {
		return []byte{}, &CheckSumError{"sha256", chunk.DecryptedShaSum, shasum}
	}

	return finalData, nil
}

func loadChunk(repository Repository, chunk Chunk) ([]byte, error) {
	if chunk.ParityParts > 0 {
		enc, err := reedsolomon.New(int(chunk.DataParts), int(chunk.ParityParts))
		if err != nil {
			return []byte{}, err
		}
		pars := make([][]byte, chunk.DataParts+chunk.ParityParts)
		parsFound := uint(0)
		parsMissing := 0
		for i := 0; i < int(chunk.DataParts+chunk.ParityParts); i++ {
			var cerr error
			pars[i], cerr = repository.Backend.LoadChunk(chunk, uint(i))
			if cerr != nil {
				pars[i] = nil
				parsMissing++
				continue
			}
			parsFound++

			if parsFound >= chunk.DataParts {
				var b bytes.Buffer
				bufWriter := bufio.NewWriter(&b)

				if parsMissing > 0 {
					err = enc.Reconstruct(pars)
					if err != nil {
						continue
					}
				}
				err = enc.Join(bufWriter, pars, chunk.Size)
				if err != nil {
					continue
				}
				bufWriter.Flush()
				return decodeChunk(repository, chunk, b.Bytes())
			}
		}

		return []byte{}, &DataReconstructionError{chunk, parsFound, chunk.DataParts - parsFound}
	}

	data, err := repository.Backend.LoadChunk(chunk, 0)
	if err != nil {
		return []byte{}, err
	}
	return decodeChunk(repository, chunk, data)
}

// DecodeArchive restores a single archive to path
func DecodeArchive(progress chan Progress, repository Repository, arc ItemData, path string) error {
	prog := Progress{}
	prog.Path = arc.Path

	if arc.Type == Directory {
		//fmt.Printf("Creating directory %s\n", path)
		os.MkdirAll(path, arc.Mode)
		prog.Statistics.Dirs++
	} else if arc.Type == SymLink {
		//fmt.Printf("Creating symlink %s -> %s\n", path, arc.PointsTo)
		os.Symlink(arc.PointsTo, path)
		prog.Statistics.SymLinks++
	} else if arc.Type == File {
		prog.Statistics.Files++
		prog.Statistics.StorageSize = arc.StorageSize
		prog.StorageSize = arc.StorageSize

		parts := uint(len(arc.Chunks))
		//fmt.Printf("Creating file %s (%d chunks).\n", path, parts)

		// write to disk
		os.MkdirAll(filepath.Dir(path), 0755)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, arc.Mode)
		if err != nil {
			return err
		}

		for i := uint(0); i < parts; i++ {
			idx, erri := indexOfChunk(arc, i)
			if erri != nil {
				return erri
			}

			chunk := arc.Chunks[idx]
			data, errc := loadChunk(repository, chunk)
			if errc != nil {
				return errc
			}

			// write/save buffer to disk
			_, err = f.Write(data)
			if err != nil {
				return err
			}

			prog.Statistics.Size += uint64(len(data))
			prog.Size += uint64(len(data))
			progress <- prog
			// fmt.Printf("Chunk OK: %d bytes, sha256: %s\n", size, chunk.DecryptedShaSum)
		}

		f.Sync()
		f.Close()

		// Restore modification time
		err = os.Chtimes(path, arc.ModTime, arc.ModTime)
		if err != nil {
			return err
		}
	}

	// Restore ownerships
	return os.Lchown(path, int(arc.UID), int(arc.GID))
}

var (
	cache map[string][]byte
	mutex = &sync.Mutex{}
)

func init() {
	cache = make(map[string][]byte)

}

// DecodeArchiveData returns the content of a single archive
func DecodeArchiveData(repository Repository, arc ItemData) (dat []byte, stats Stats, err error) {
	if arc.Type == File {
		parts := uint(len(arc.Chunks))

		for i := uint(0); i < parts; i++ {
			idx, err := indexOfChunk(arc, i)
			if err != nil {
				return dat, stats, err
			}

			chunk := arc.Chunks[idx]
			mutex.Lock()
			cacheData, ok := cache[chunk.ShaSum]
			if ok {
				fmt.Println("Using cached chunk", chunk.ShaSum)
				dat = append(dat, cacheData...)
				mutex.Unlock()
			} else {
				finalData, err := loadChunk(repository, chunk)
				if err != nil {
					return dat, stats, err
				}
				dat = append(dat, finalData...)
				cache[chunk.ShaSum] = finalData
				mutex.Unlock()
			}

			stats.StorageSize += uint64(len(dat))
			stats.Size += uint64(len(dat))
		}

		stats.Files++
	}

	return dat, stats, nil
}

func readArchiveChunk(repository Repository, arc ItemData, chunkNum uint) (dat *[]byte, err error) {
	dat = &[]byte{}
	idx, err := indexOfChunk(arc, chunkNum)
	if err != nil {
		return dat, err
	}

	chunk := arc.Chunks[idx]
	mutex.Lock()
	cacheData, ok := cache[chunk.ShaSum]
	if ok {
		// fmt.Println("Using cached chunk", chunk.ShaSum)
		*dat = append(*dat, cacheData...)
		mutex.Unlock()
		return dat, nil
	}

	finalData, err := loadChunk(repository, chunk)
	if err != nil {
		return dat, err
	}

	*dat = append(*dat, finalData...)
	cache[chunk.ShaSum] = finalData
	mutex.Unlock()

	return dat, nil
}

func indexOfChunk(arc ItemData, chunkNum uint) (int, error) {
	for i, chunk := range arc.Chunks {
		if chunk.Num == chunkNum {
			return i, nil
		}
	}

	return 0, &ChunkError{chunkNum}
}

func chunkForOffset(arc ItemData, offset int) (uint, int, error) {
	size := 0
	for i := 0; i < len(arc.Chunks); i++ {
		idx, err := indexOfChunk(arc, uint(i))
		if err != nil {
			return 0, 0, &SeekError{offset}
		}

		chunk := arc.Chunks[idx]
		if size+chunk.OriginalSize > offset {
			internalOffset := offset - size
			return chunk.Num, internalOffset, nil
		}

		size += chunk.OriginalSize
	}

	return 0, 0, io.EOF
}

// ReadArchive reads from an archive
func ReadArchive(repository Repository, arc ItemData, offset int, size int) (dat *[]byte, err error) {
	dat = &[]byte{}
	//	fmt.Println("Read req:", offset, size)
	if arc.Type == File {
		neededPart, internalOffset, err := chunkForOffset(arc, offset)
		if err != nil {
			return dat, err
		}

		for len(*dat) < size {
			b, err := readArchiveChunk(repository, arc, neededPart)
			if err != nil || len(*b) == 0 {
				//return dat, err
				panic(err)
			}

			d := *b
			d = d[internalOffset:]
			if err != nil || len(d) == 0 {
				//return dat, err
				panic(err)
			}
			if len(d)+len(*dat) > size {
				*dat = append(*dat, d[:size-len(*dat)]...)
			} else {
				*dat = append(*dat, d...)
			}

			internalOffset = 0
			neededPart++
		}

		// cache the next block NOW
		go func() {
			readArchiveChunk(repository, arc, neededPart)
		}()
	}

	return dat, nil
}
