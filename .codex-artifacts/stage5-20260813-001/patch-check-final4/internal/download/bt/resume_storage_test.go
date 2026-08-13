package btdownload

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	torrentstorage "github.com/anacrolix/torrent/storage"
)

func TestResumeFileStorageSpansFilesPersistsAndReleasesHandles(t *testing.T) {
	info := metainfo.Info{
		Name:        "bundle",
		PieceLength: 8,
		Pieces:      make([]byte, 2*metainfo.HashSize),
		Files: []metainfo.FileInfo{
			{Length: 5, Path: []string{"one.bin"}},
			{Length: 5, Path: []string{"two.bin"}},
		},
	}
	metaFiles := info.UpvertedFiles()
	base := t.TempDir()
	completion := torrentstorage.NewMapPieceCompletion()
	client := newResumeFileStorage(base, map[string]string{
		metainfoFileKey(metaFiles[0]): filepath.Join("renamed", "one.bin"),
		metainfoFileKey(metaFiles[1]): filepath.Join("renamed", "two.bin"),
	}, completion)
	opened, err := client.OpenTorrent(context.Background(), &info, metainfo.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	piece := opened.Piece(info.Piece(0))
	want := []byte("abcdefgh")
	if written, err := piece.WriteAt(want, 0); err != nil || written != len(want) {
		t.Fatalf("write=%d err=%v", written, err)
	}
	if err := piece.MarkComplete(); err != nil {
		t.Fatal(err)
	}
	if got := piece.Completion(); got.Err != nil || !got.Ok || !got.Complete {
		t.Fatalf("completion=%#v", got)
	}
	read := make([]byte, len(want))
	if count, err := piece.ReadAt(read, 0); err != nil || count != len(read) || !bytes.Equal(read, want) {
		t.Fatalf("read=%q count=%d err=%v", read, count, err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "renamed")
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("storage retained a Windows file handle: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("storage root still exists: %v", err)
	}
}
