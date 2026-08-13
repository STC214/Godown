package btdownload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	torrentstorage "github.com/anacrolix/torrent/storage"
)

// resumeFileStorage deliberately opens files per operation. The upstream file
// backend's default mmap cache retains Windows handles for the process lifetime,
// which prevents deterministic pause/redownload cleanup. Piece completion is
// still durable through the supplied Bolt-backed implementation.
type resumeFileStorage struct {
	baseDir    string
	paths      map[string]string
	completion torrentstorage.PieceCompletion
	closeOnce  sync.Once
	closeErr   error
}

type resumeFileEntry struct {
	path   string
	offset int64
	length int64
}

type resumeTorrentStorage struct {
	infoHash   metainfo.Hash
	files      []resumeFileEntry
	completion torrentstorage.PieceCompletion
}

type resumePiece struct {
	torrent *resumeTorrentStorage
	piece   metainfo.Piece
}

func newResumeFileStorage(baseDir string, paths map[string]string, completion torrentstorage.PieceCompletion) torrentstorage.ClientImplCloser {
	clonedPaths := make(map[string]string, len(paths))
	for key, value := range paths {
		clonedPaths[key] = value
	}
	return &resumeFileStorage{baseDir: baseDir, paths: clonedPaths, completion: completion}
}

func (s *resumeFileStorage) OpenTorrent(_ context.Context, info *metainfo.Info, infoHash metainfo.Hash) (torrentstorage.TorrentImpl, error) {
	metaFiles := info.UpvertedFiles()
	files := make([]resumeFileEntry, 0, len(metaFiles))
	for _, metaFile := range metaFiles {
		relative, ok := s.paths[metainfoFileKey(metaFile)]
		if !ok || relative == "" {
			return torrentstorage.TorrentImpl{}, fmt.Errorf("BitTorrent storage path missing for %q", metaFile.DisplayPath(info))
		}
		fullPath := filepath.Join(s.baseDir, relative)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return torrentstorage.TorrentImpl{}, fmt.Errorf("create BitTorrent storage directory: %w", err)
		}
		if stat, err := os.Stat(fullPath); err == nil {
			if !stat.Mode().IsRegular() {
				return torrentstorage.TorrentImpl{}, fmt.Errorf("BitTorrent output path is not a regular file: %q", fullPath)
			}
			if stat.Size() > metaFile.Length {
				if err := os.Truncate(fullPath, metaFile.Length); err != nil {
					return torrentstorage.TorrentImpl{}, fmt.Errorf("trim BitTorrent output file: %w", err)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return torrentstorage.TorrentImpl{}, fmt.Errorf("inspect BitTorrent output file: %w", err)
		} else if metaFile.Length == 0 {
			file, createErr := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR, 0o644)
			if createErr != nil {
				return torrentstorage.TorrentImpl{}, createErr
			}
			if closeErr := file.Close(); closeErr != nil {
				return torrentstorage.TorrentImpl{}, closeErr
			}
		}
		files = append(files, resumeFileEntry{path: fullPath, offset: metaFile.TorrentOffset, length: metaFile.Length})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].offset < files[right].offset })
	torrentStorage := &resumeTorrentStorage{infoHash: infoHash, files: files, completion: s.completion}
	return torrentstorage.TorrentImpl{
		Piece: func(piece metainfo.Piece) torrentstorage.PieceImpl {
			return &resumePiece{torrent: torrentStorage, piece: piece}
		},
		Close: func() error { return nil },
	}, nil
}

func (s *resumeFileStorage) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.completion.Close() })
	return s.closeErr
}

func (p *resumePiece) ReadAt(buffer []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, os.ErrInvalid
	}
	if offset >= p.piece.Length() {
		return 0, io.EOF
	}
	buffer = buffer[:min64(int64(len(buffer)), p.piece.Length()-offset)]
	read, err := p.torrent.readAt(buffer, p.piece.Offset()+offset)
	if err == nil && read < len(buffer) {
		err = io.EOF
	}
	return read, err
}

func (p *resumePiece) WriteAt(buffer []byte, offset int64) (int, error) {
	if offset < 0 || offset+int64(len(buffer)) > p.piece.Length() {
		return 0, os.ErrInvalid
	}
	return p.torrent.writeAt(buffer, p.piece.Offset()+offset)
}

func (p *resumePiece) Completion() torrentstorage.Completion {
	key := metainfo.PieceKey{InfoHash: p.torrent.infoHash, Index: p.piece.Index()}
	completion, err := p.torrent.completion.Get(key)
	completion.Err = errors.Join(completion.Err, err)
	if completion.Err != nil || !completion.Ok || !completion.Complete {
		return completion
	}
	if err := p.torrent.checkExtent(p.piece.Offset(), p.piece.Length()); err != nil {
		completion.Complete = false
		completion.Err = errors.Join(completion.Err, p.torrent.completion.Set(key, false))
	}
	return completion
}

func (p *resumePiece) MarkComplete() error {
	if err := p.torrent.syncExtent(p.piece.Offset(), p.piece.Length()); err != nil {
		return err
	}
	return p.torrent.completion.Set(metainfo.PieceKey{InfoHash: p.torrent.infoHash, Index: p.piece.Index()}, true)
}

func (p *resumePiece) MarkNotComplete() error {
	return p.torrent.completion.Set(metainfo.PieceKey{InfoHash: p.torrent.infoHash, Index: p.piece.Index()}, false)
}

func (s *resumeTorrentStorage) readAt(buffer []byte, globalOffset int64) (int, error) {
	written := 0
	for len(buffer) > 0 {
		file, available := s.entryAt(globalOffset)
		if file == nil {
			if available <= 0 {
				return written, io.EOF
			}
			chunk := min64(int64(len(buffer)), available)
			clear(buffer[:chunk])
			buffer = buffer[chunk:]
			globalOffset += chunk
			written += int(chunk)
			continue
		}
		chunk := min64(int64(len(buffer)), available)
		handle, err := os.Open(file.path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return written, io.EOF
			}
			return written, err
		}
		read, readErr := handle.ReadAt(buffer[:chunk], globalOffset-file.offset)
		closeErr := handle.Close()
		written += read
		buffer = buffer[read:]
		globalOffset += int64(read)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return written, errors.Join(readErr, closeErr)
		}
		if closeErr != nil {
			return written, closeErr
		}
		if int64(read) != chunk {
			return written, io.EOF
		}
	}
	return written, nil
}

func (s *resumeTorrentStorage) writeAt(buffer []byte, globalOffset int64) (int, error) {
	written := 0
	for len(buffer) > 0 {
		file, available := s.entryAt(globalOffset)
		if file == nil {
			if available <= 0 {
				return written, io.ErrShortWrite
			}
			chunk := min64(int64(len(buffer)), available)
			buffer = buffer[chunk:]
			globalOffset += chunk
			written += int(chunk)
			continue
		}
		chunk := min64(int64(len(buffer)), available)
		handle, err := os.OpenFile(file.path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return written, err
		}
		count, writeErr := handle.WriteAt(buffer[:chunk], globalOffset-file.offset)
		closeErr := handle.Close()
		written += count
		buffer = buffer[count:]
		globalOffset += int64(count)
		if writeErr != nil || closeErr != nil {
			return written, errors.Join(writeErr, closeErr)
		}
		if int64(count) != chunk {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (s *resumeTorrentStorage) entryAt(globalOffset int64) (*resumeFileEntry, int64) {
	for index := range s.files {
		file := &s.files[index]
		if globalOffset < file.offset {
			return nil, file.offset - globalOffset
		}
		if globalOffset < file.offset+file.length {
			return file, file.offset + file.length - globalOffset
		}
	}
	return nil, 0
}

func (s *resumeTorrentStorage) checkExtent(offset, length int64) error {
	end := offset + length
	for _, file := range s.files {
		start := max64(offset, file.offset)
		stop := min64(end, file.offset+file.length)
		if stop <= start {
			continue
		}
		stat, err := os.Stat(file.path)
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() || stat.Size() < stop-file.offset {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}

func (s *resumeTorrentStorage) syncExtent(offset, length int64) error {
	end := offset + length
	var result error
	for _, file := range s.files {
		if min64(end, file.offset+file.length) <= max64(offset, file.offset) {
			continue
		}
		handle, err := os.OpenFile(file.path, os.O_RDWR, 0o644)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		result = errors.Join(result, handle.Sync(), handle.Close())
	}
	return result
}
