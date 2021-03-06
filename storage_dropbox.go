/*
 * knoxite
 *     Copyright (c) 2016, Christian Muehlhaeuser <muesli@gmail.com>
 *     Copyright (c) 2016, Nicolas Martin <penguwingithub@gmail.com>
 *
 *   For license see LICENSE.txt
 */

package knoxite

import (
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/stacktic/dropbox"
)

// StorageDropbox stores data on a remote Dropbox
type StorageDropbox struct {
	url            url.URL
	chunkPath      string
	snapshotPath   string
	repositoryPath string
	db             *dropbox.Dropbox
}

// NewStorageDropbox returns a StorageDropbox object
func NewStorageDropbox(u url.URL) *StorageDropbox {
	storage := StorageDropbox{
		url:            u,
		chunkPath:      filepath.Join(u.Path, "chunks"),
		snapshotPath:   filepath.Join(u.Path, "snapshots"),
		repositoryPath: filepath.Join(u.Path, repoFilename),
		db:             dropbox.NewDropbox(),
	}

	ak, _ := base64.StdEncoding.DecodeString("aXF1bGs0a25vajIydGtt")
	as, _ := base64.StdEncoding.DecodeString("N3htbmlhcDV0cmE5NTE5")
	storage.db.SetAppInfo(string(ak), string(as))

	if storage.url.User == nil || len(storage.url.User.Username()) == 0 {
		if err := storage.db.Auth(); err != nil {
			panic(err)
		}
		storage.url.User = url.User(storage.db.AccessToken())
	} else {
		storage.db.SetAccessToken(storage.url.User.Username())
	}

	return &storage
}

// Location returns the type and location of the repository
func (backend *StorageDropbox) Location() string {
	return backend.url.String()
}

// Close the backend
func (backend *StorageDropbox) Close() error {
	return nil
}

// Protocols returns the Protocol Schemes supported by this backend
func (backend *StorageDropbox) Protocols() []string {
	return []string{"dropbox"}
}

// Description returns a user-friendly description for this backend
func (backend *StorageDropbox) Description() string {
	return "Dropbox Storage"
}

// AvailableSpace returns the free space on this backend
func (backend *StorageDropbox) AvailableSpace() (uint64, error) {
	account, err := backend.db.GetAccountInfo()
	if err != nil {
		return 0, err
	}

	return uint64(account.QuotaInfo.Quota - account.QuotaInfo.Shared - account.QuotaInfo.Normal), nil
}

// LoadChunk loads a Chunk from dropbox
func (backend *StorageDropbox) LoadChunk(shasum string, part, totalParts uint) (*[]byte, error) {
	path := filepath.Join(backend.chunkPath, SubDirForChunk(shasum))
	fileName := filepath.Join(path, shasum+"."+strconv.FormatUint(uint64(part), 10)+"_"+strconv.FormatUint(uint64(totalParts), 10))
	obj, _, err := backend.db.Download(fileName, "", 0)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(obj)
	return &data, err
}

// StoreChunk stores a single Chunk on dropbox
func (backend *StorageDropbox) StoreChunk(shasum string, part, totalParts uint, data *[]byte) (uint64, error) {
	path := filepath.Join(backend.chunkPath, SubDirForChunk(shasum))
	backend.db.CreateFolder(path)

	fileName := filepath.Join(path, shasum+"."+strconv.FormatUint(uint64(part), 10)+"_"+strconv.FormatUint(uint64(totalParts), 10))
	if entry, err := backend.db.Metadata(fileName, false, false, "", "", 1); err == nil {
		// Chunk is already stored
		if int(entry.Bytes) == len(*data) {
			return 0, nil
		}
	}

	//FIXME: this doesn't really chunk anything - it always picks the full data block's size
	entry, err := backend.db.UploadByChunk(ioutil.NopCloser(bytes.NewReader(*data)), len(*data), fileName, true, "")
	return uint64(entry.Bytes), err
}

// LoadSnapshot loads a snapshot
func (backend *StorageDropbox) LoadSnapshot(id string) ([]byte, error) {
	path := filepath.Join(backend.snapshotPath, id)
	// Getting obj as type io.ReadCloser and reading it out in order to get bytes returned
	obj, _, err := backend.db.Download(path, "", 0)
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(obj)
}

// SaveSnapshot stores a snapshot
func (backend *StorageDropbox) SaveSnapshot(id string, data []byte) error {
	path := filepath.Join(backend.snapshotPath, id)
	_, err := backend.db.UploadByChunk(ioutil.NopCloser(bytes.NewReader(data)), len(data), path, true, "")
	return err
}

// InitRepository creates a new repository
func (backend *StorageDropbox) InitRepository() error {
	if _, err := backend.db.CreateFolder(backend.url.Path); err != nil {
		return ErrRepositoryExists
	}
	if _, err := backend.db.CreateFolder(backend.snapshotPath); err != nil {
		return ErrRepositoryExists
	}
	if _, err := backend.db.CreateFolder(backend.chunkPath); err != nil {
		return ErrRepositoryExists
	}
	return nil
}

// LoadRepository reads the metadata for a repository
func (backend *StorageDropbox) LoadRepository() ([]byte, error) {
	obj, _, err := backend.db.Download(backend.repositoryPath, "", 0)
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(obj)
}

// SaveRepository stores the metadata for a repository
func (backend *StorageDropbox) SaveRepository(data []byte) error {
	_, err := backend.db.UploadByChunk(ioutil.NopCloser(bytes.NewReader(data)), len(data), backend.repositoryPath, true, "")
	return err
}
