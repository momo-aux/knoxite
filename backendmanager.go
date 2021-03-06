/*
 * knoxite
 *     Copyright (c) 2016, Christian Muehlhaeuser <muesli@gmail.com>
 *
 *   For license see LICENSE.txt
 */

package knoxite

import "errors"

// BackendManager stores data on multiple backends
type BackendManager struct {
	Backends []*Backend

	lastUsedBackend int
}

// Error declarations
var (
	ErrLoadChunkFailed      = errors.New("Unable to load chunk from any storage backend")
	ErrLoadSnapshotFailed   = errors.New("Unable to load repository from any storage backend")
	ErrLoadRepositoryFailed = errors.New("Unable to load repository from any storage backend")
)

// AddBackend adds a backend
func (backend *BackendManager) AddBackend(be *Backend) {
	backend.Backends = append(backend.Backends, be)
}

// Locations returns the urls for all backends
func (backend *BackendManager) Locations() []string {
	paths := []string{}
	for _, be := range backend.Backends {
		paths = append(paths, (*be).Location())
	}

	return paths
}

// LoadChunk loads a Chunk from backends
func (backend *BackendManager) LoadChunk(chunk Chunk, part uint) ([]byte, error) {
	for _, be := range backend.Backends {
		b, err := (*be).LoadChunk(chunk.ShaSum, uint(part), chunk.DataParts)
		if err == nil {
			return *b, err
		}
	}

	return []byte{}, ErrLoadChunkFailed
}

// StoreChunk stores a single Chunk on backends
func (backend *BackendManager) StoreChunk(chunk Chunk) (size uint64, err error) {
	for i, data := range *chunk.Data {
		// Use storage backends in a round robin fashion to store chunks
		backend.lastUsedBackend++
		if backend.lastUsedBackend+1 > len(backend.Backends) {
			backend.lastUsedBackend = 0
		}

		be := backend.Backends[backend.lastUsedBackend]
		//	for _, be := range backend.Backends {
		_, err = (*be).StoreChunk(chunk.ShaSum, uint(i), chunk.DataParts, &data)
		if err != nil {
			return 0, err
		}
		//	}
	}

	return uint64(chunk.Size), nil
}

// LoadSnapshot loads a snapshot
func (backend *BackendManager) LoadSnapshot(id string) ([]byte, error) {
	for _, be := range backend.Backends {
		b, err := (*be).LoadSnapshot(id)
		if err == nil {
			return b, err
		}
	}

	return []byte{}, ErrLoadSnapshotFailed
}

// SaveSnapshot stores a snapshot on all storage backends
func (backend *BackendManager) SaveSnapshot(id string, b []byte) error {
	for _, be := range backend.Backends {
		err := (*be).SaveSnapshot(id, b)
		if err != nil {
			return err
		}
	}

	return nil
}

// InitRepository creates a new repository
func (backend *BackendManager) InitRepository() error {
	for _, be := range backend.Backends {
		err := (*be).InitRepository()
		if err != nil {
			return err
		}
	}

	return nil
}

// LoadRepository reads the metadata for a repository
func (backend *BackendManager) LoadRepository() ([]byte, error) {
	for _, be := range backend.Backends {
		b, err := (*be).LoadRepository()
		if err == nil {
			return b, err
		}
	}

	return []byte{}, ErrLoadRepositoryFailed
}

// SaveRepository stores the metadata for a repository
func (backend *BackendManager) SaveRepository(b []byte) error {
	for _, be := range backend.Backends {
		err := (*be).SaveRepository(b)
		if err != nil {
			return err
		}
	}

	return nil
}
