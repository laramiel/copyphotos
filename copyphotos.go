// This utility copies photos into a date-specific folder
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"regexp"
)

import (
	// go get github.com/rwcarlsen/goexif/exif
	// go get github.com/rwcarlsen/goexif/tiff
	"github.com/rwcarlsen/goexif/exif"
)

const form = "2006:01:02 15:04:05"

var (
	dryRun         = true
	folder_format  = "2006_01_02"
	raw_format     = "2006_01_02_RAW"
	mov_format     = "2006_01_02_MOV"
	exclude_pattern *regexp.Regexp = nil
)

// PhotoType indicates whether the photo is a RAW, JPG, TIF or MOV.
type PhotoType int

const (
	kNone PhotoType = iota
	kJpg
	kRaw
	kTif
	kMov
)

type CopyMode int

const (
	mCopy CopyMode = iota
	mMove
	mDeleteIfExists
	mCopyAndDeleteIfExists
)

// GetPhotoType returns the PhotoType from the file extension.
func GetPhotoType(ext string) PhotoType {
	if ext == ".jpg" ||
		ext == ".jpeg" {
		return kJpg
	} else if ext == ".tiff" ||
		ext == ".tif" {
		return kTif
	} else if ext == ".nef" ||
		ext == ".rw2" ||
		ext == ".cr2" ||
		ext == ".crw" {
		return kRaw
	} else if ext == ".mov" ||
		ext == ".mpg" {
		return kMov
	}
	return kNone
}

// cp copies the src file to the dst.
func cp(dst, src string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	// no need to check errors on read only file, we already got everything
	// we need from the filesystem, so nothing can go wrong now.
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Sync()
}

// exists returns whether the file or directory exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

type pathEntry struct {
	filename string
	ptype    PhotoType
}

// pathWalker walks the filesystem, queueing pathEntry items onto the queue.
type pathWalker struct {
	queue chan pathEntry
}

func (p *pathWalker) producer(path string, info os.FileInfo, _ error) error {
	if info.IsDir() {
		return nil
	}
	lowerpath := strings.ToLower(path)
	if exclude_pattern != nil && exclude_pattern.MatchString(lowerpath) {
		return nil
	}
	ptype := GetPhotoType(filepath.Ext(lowerpath))
	if ptype == kNone {
		return nil
	}
	p.queue <- pathEntry{path, ptype}
	return nil
}

func (p *pathWalker) Start(path string) {
	p.queue = make(chan pathEntry, 32)
	go func() {
		filepath.Walk(path, p.producer)
		close(p.queue)
	}()
}

type moveEntry struct {
	source string
	dest   string
}

type fileMover struct {
	sourcePath string
	destPath   string
	CopyMode   CopyMode
	cpLarge    bool
	queue      chan moveEntry
}

func (m *fileMover) listener(queue chan pathEntry, wg *sync.WaitGroup) {
	for e := range queue {
		m.decode(e.filename, e.ptype)
	}
	wg.Done()
}

func (m *fileMover) decode(path string, ptype PhotoType) error {
	// Attempt to open the file for reading.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// First, stat the file.
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	t := stat.ModTime()

	// Next, read the exif data.
	x, err := exif.Decode(f)
	if err != nil {
		return err
	}

	// Try and read exif field names instead of just the filename.
	for _, v := range []exif.FieldName{"DateTimeOriginal", "DateTimeDigitized", "DateTime"} {
		tag, err := x.Get(v)
		if err != nil {
			continue
		}
		val, err := tag.StringVal()
		if err != nil {
			continue
		}
		tm, err := time.Parse(form, val)
		if err != nil {
			continue
		}
		t = tm
		break
	}

	// Generate the output folder name
	var folder string
	switch ptype {
	case kRaw:
		folder = t.Format(raw_format)
		break
	case kMov:
		folder = t.Format(mov_format)
		break
	default:
		folder = t.Format(folder_format)
	}
	dest := filepath.Join(m.destPath, folder, filepath.Base(path))
	if exists(dest) {
		return nil
	}
	m.queue <- moveEntry{path, dest}
	return nil
}

// Move consumes the
func (m *fileMover) move(e moveEntry) {
	// Test existance of dest.
	dinfo, err := os.Stat(e.dest)
	if err == nil {
		sinfo, err := os.Stat(e.source)
		if err == nil && sinfo.Size() > dinfo.Size() {
			if !m.cpLarge {
				fmt.Printf("diff %v %v\n", e.source, e.dest)
				return
			}
		} else {
			return
		}
	}
	if !os.IsNotExist(err) {
		return
	}

	dest_dir := filepath.Dir(e.dest)
	dir_exists := exists(dest_dir)
	if !dir_exists {
		fmt.Printf("mkdir -p %v\n", dest_dir)
	}
	fmt.Printf("mv %v %v\n", e.source, e.dest)

	if !dryRun {
		if !dir_exists {
			os.MkdirAll(dest_dir, 0777)
		}
		os.Rename(e.source, e.dest)
	}
}

func (m *fileMover) copy(e moveEntry) {
	// Test existance of dest.
	dinfo, err := os.Stat(e.dest)
	if err == nil {
		sinfo, err := os.Stat(e.source)
		if err == nil && sinfo.Size() > dinfo.Size() {
			if !m.cpLarge {
				fmt.Printf("diff %v %v\n", e.source, e.dest)
				return
			}
		} else {
			return
		}
	}
	if !os.IsNotExist(err) {
		return
	}
	dest_dir := filepath.Dir(e.dest)
	dir_exists := exists(dest_dir)
	if !dir_exists {
		fmt.Printf("mkdir -p %v\n", dest_dir)
	}
	fmt.Printf("cp %v %v\n", e.source, e.dest)

	if !dryRun {
		if !dir_exists {
			os.MkdirAll(dest_dir, 0777)
		}
		cp(e.dest, e.source)
	}
}

func (m *fileMover) deleteIfExists(e moveEntry) {
	dinfo, err := os.Stat(e.dest)
	if err != nil {
		// File does not exist; not readable.
		return
	}
	sinfo, err := os.Stat(e.source)
	if err != nil {
		// File does not exist; can't remove it.
		return
	}
	if os.SameFile(sinfo, dinfo) {
		// Same file; don't remove.
		return
	}
	if sinfo.Size() != dinfo.Size() {
		// Not same size; don't remove.
		return
	}
	// TODO: checksum both files?
	fmt.Printf("rm %v\n", e.source)
	if !dryRun {
		os.Remove(e.source)
	}
}

// Run the copy/move operation.
func (m *fileMover) Run() {
	var walker pathWalker
	walker.Start(m.sourcePath)
	m.queue = make(chan moveEntry, 16)

	// start several threads stating files.
	var wg1 sync.WaitGroup
	wg1.Add(4)
	go m.listener(walker.queue, &wg1)
	go m.listener(walker.queue, &wg1)
	go m.listener(walker.queue, &wg1)
	go m.listener(walker.queue, &wg1)
	go func() {
		wg1.Wait()
		close(m.queue)
	}()

	// start several threads copying files
	var wg2 sync.WaitGroup
	switch m.CopyMode {
	case mCopy:
		f := func() {
			for e := range m.queue {
				m.copy(e)
			}
			wg2.Done()
		}
		wg2.Add(2)
		go f()
		go f()
	case mMove:
		f := func() {
			for e := range m.queue {
				m.move(e)
			}
			wg2.Done()
		}
		wg2.Add(2)
		go f()
		go f()

	case mDeleteIfExists:
		f := func() {
			for e := range m.queue {
				m.deleteIfExists(e)
			}
			wg2.Done()
		}
		wg2.Add(1)
		go f()
	}
	wg2.Wait()
}

const kUsage = `
Usage: %s [-n][-cp] <src> <dest>
  -n       dryrun
  -cp      copy [default is move]
  -del     delete [default is move]
  -large   copy larger files
  -x       eXclude regularexpression match
  -f  %s
  -r  %s
  -m  %s

  <src>   source path
  <dest>  destination path
`

func main() {
	var flag_cp bool
	var flag_del bool
	var flag_large bool
	var exclude string
	flag.BoolVar(&dryRun, "n", false, "Dryrun")
	flag.BoolVar(&flag_cp, "cp", false, "Copy, don't move.")
	flag.BoolVar(&flag_del, "del", false, "Delete if exists.")
	flag.BoolVar(&flag_large, "large", false, "Copy larger files.")

	// Setup the folder formats
	flag.StringVar(&folder_format, "f", "2006/2006-01-02", "Basic format")
	flag.StringVar(&raw_format, "r", "2006/2006-01-02", "Raw format")
	flag.StringVar(&mov_format, "m", "2006/2006-01-02", "Mov format")
    flag.StringVar(&exclude, "x", "", "Exclude files which match this regexp")

	flag.Parse()

    if exclude != "" {
    	exclude_pattern = regexp.MustCompile(exclude)
    }
	if flag.NArg() != 2 {
		fmt.Printf(kUsage, os.Args[0], folder_format, raw_format, mov_format)
		return
	}
	for i := 0; i < 2; i++ {
		if !exists(flag.Arg(i)) {
			fmt.Printf("Path does not exist: %s\n", flag.Arg(i))
		}
	}

	var mode CopyMode = mMove
	if flag_cp {
		mode = mCopy
		fmt.Printf("Mode: copy\n")
	} else if flag_del {
		mode = mDeleteIfExists
		fmt.Printf("Mode: delete\n")
	} else {
		fmt.Printf("Mode: move\n")
	}
	fileMover := &fileMover{
		flag.Arg(0),
		flag.Arg(1),
		mode,
		flag_large,
		nil,
	}
	fileMover.Run()
}
