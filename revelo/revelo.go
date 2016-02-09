/*
 * TODO:
 * 1. Preserve FILE, HANDLE across lookup, open, create
 * 	- Need this to support O_EXCL and other semantics
 * 	- Multiple simul access is not handled properly
 */

package revelo

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/net/context"

	"github.com/muthu-r/horcrux/bazil-fuse/fuse"
	"github.com/muthu-r/horcrux/bazil-fuse/fuse/fs"

	log "github.com/Sirupsen/logrus"

	"github.com/muthu-r/horcrux"
	"github.com/muthu-r/horcrux/revelo/dirTree"

	"github.com/muthu-r/horcrux/accio"
	"github.com/muthu-r/horcrux/accio/minio"
	"github.com/muthu-r/horcrux/accio/cp"
	"github.com/muthu-r/horcrux/accio/s3"
	"github.com/muthu-r/horcrux/accio/scp"
)

type ReveloData struct {
	Config   horcrux.Config
	NumFiles int
	CurrVer  string

	metaName string // Name of the meta file - input to revelo

	Root *dirTree.Node // DirTree for FS ops
	lock sync.RWMutex  // Lock for the tree

	cacheDir string
	mntDir   string
	fuseConn *fuse.Conn
}

var GlobalData ReveloData

var Usage = func() {
	fmt.Printf("Usage: revelo <name> <access-type> <mnt-dir>\n" +
		"            access-type is one of:\n" +
		"                cp://<local-dir>\n" +
		"                scp://user::passwd@host:/path\n" +
		"                s3://bucket@region (credentials in ~/.s3/credentials)\n" +
		"                minio://host:port/bucket (credentials in ~/.minio/horcrux.json)\n")
}

// Given a string <accType>://<args>, it returns accType, args
func getAccessType(acc string) (string, string) {

	Idx := strings.Index(acc, ":")
	if Idx == 0 || Idx == -1 {
		return "", acc
	}

	if acc[Idx+1:Idx+3] != "//" {
		return "", acc
	}

	switch acc[:Idx] {
	case "cp":
		return "cp", acc[Idx+3:]
	case "scp":
		return "scp", acc[Idx+3:]
	case "s3":
		return "s3", acc[Idx+3:]
	case "minio":
		return "minio", acc[Idx+3:]
	}

	return acc[:Idx], acc[Idx+3:]
}

// Create Accio interface and set args
func initAccess(accType string, mntDir string) (accio.Access, error) {

	var acc accio.Access

	md, args := getAccessType(accType)
	switch md {
	case "cp":
		acc = &cp.Data{SrcDir: args}
	case "scp":
		acc = &scp.Data{ScpArgs: args}
	case "s3":
		acc = &s3.Data{S3Args: args}
	case "minio":
		acc = &minio.Data{MinioArgs: args}
	case "":
		log.Error("Revelo - Bad Arguments")
		Usage()
		return nil, syscall.EINVAL
	default:
		log.WithFields(log.Fields{
			"Args":   args,
			"Method": md,
		}).Error("initAccess: Access method Not implemented")
		return nil, syscall.EINVAL
	}

	return acc, nil
}

//
// Main function.
//  - Exposes remote FS structure locally using FUSE (bazil-fuse)
//  - Gets files from remote on-demand
//  - TODO:
//	- Check-in files to remote
//	- Version control
//
func Revelo(Name string, accType string, cacheDir string, mntDir string) error {

	GlobalData.metaName = Name + ".meta"
	acc, err := initAccess(accType, mntDir)
	if err != nil {
		log.Errorf("Revelo: Invalid Access type: %v", accType)
		return err
	}

	remoteDir, err := acc.Init()
	if err != nil {
		log.WithFields(log.Fields{"Acc": acc, "Error": err}).Error("Revelo: Cannot init access")
		return err
	}

	GlobalData.cacheDir = cacheDir
	GlobalData.mntDir = mntDir

	log.WithFields(log.Fields{
		"Access":    acc,
		"CacheDir":  cacheDir,
		"mntDir":    mntDir,
		"RemoteDir": remoteDir,
	}).Info("Revelo - Init done...")

	_, err = os.Stat(cacheDir + "/" + GlobalData.metaName)

	// If some error happened here, we might fail in open later.
	// Its better not to GetFile in that case.

	// Get meta from <name>.meta file
	metaPresent := ((err == nil) || !os.IsNotExist(err))
	if metaPresent == false {
		var metaName string
		if remoteDir == "" {
			metaName = GlobalData.metaName
		} else {
			metaName = remoteDir + "/" + GlobalData.metaName
		}
		err = acc.GetFile(metaName, cacheDir+"/"+GlobalData.metaName)
		if err != nil {
			log.WithFields(log.Fields{
				"Remote":  metaName,
				"Local":   cacheDir + "/" + GlobalData.metaName,
				"AccData": acc,
			}).Error("Revelo: Cannot get meta file")
			return err
		}
	} else {
		log.Info("Revelo: Meta file present, using it...")
	}

	metaFile, err := os.Open(cacheDir + "/" + GlobalData.metaName)
	if err != nil {
		log.WithFields(log.Fields{
			"Meta File": cacheDir + "/" + GlobalData.metaName,
			"Error":     err,
		}).Error("Cannot open meta file")
		return err
	}
	defer metaFile.Close()

	st, _ := metaFile.Stat() //XXX This should not fail
	metaData := make([]byte, st.Size()+1)
	n, err := metaFile.Read(metaData)
	if err != nil || n == 0 {
		log.WithFields(log.Fields{
			"Meta File":  cacheDir + "/" + GlobalData.metaName,
			"Read bytes": n,
			"Error":      err,
		}).Error("Revelo: Cannot read meta file, error or empty")
		return syscall.EINVAL
	}

	// Unmarshal the meta data
	metaData = metaData[:n]
	meta := new(horcrux.Meta)
	err = json.Unmarshal(metaData, meta)
	if err != nil {
		log.WithFields(log.Fields{
			"Meta Data": string(metaData),
			"Error":     err,
		}).Error("Revelo: Cannot unmarshal meta data")
		return err
	}

	// Create dirTree
	GlobalData.Root, err = dirTree.Create(meta)
	GlobalData.Config = meta.Config
	GlobalData.CurrVer = meta.CurrVer
	GlobalData.NumFiles = meta.NumFiles

	// Mount local
	fuseConn, err := fuse.Mount(mntDir,
		fuse.FSName("Horcrux"),
		fuse.Subtype("Horcrux-"+acc.Name()),
		fuse.MaxReadahead(128 * (1 << 10)),
		fuse.AllowOther()) //XXX : Revisit AllowOther
	if err != nil {
		log.WithFields(log.Fields{"Conn": fuseConn, "Error": err}).Error("Mount Failed")
		return err
	}
	defer fuseConn.Close()

	GlobalData.fuseConn = fuseConn

	log.Debugf("Mount OK: %v", GlobalData.CurrVer)

	horcruxFS := &FS{Acc: &acc, RData: &GlobalData, remoteDir: remoteDir, cacheDir: cacheDir}

	err = fs.Serve(fuseConn, horcruxFS)
	if err != nil {
		log.WithFields(log.Fields{"Conn": fuseConn, "Error": err}).Error("Cannot fs.Serve")
		return err
	}

	<-fuseConn.Ready

	if err := fuseConn.MountError; err != nil {
		log.WithFields(log.Fields{"Conn": fuseConn, "Error": err}).Error("Mount Failed")
		return err
	}

	log.Info("Revelo Done...")
	return nil
}

// Unmount local
func Unmount(mntDir string) error {
	if err := fuse.Unmount(mntDir); err != nil {
		log.WithFields(log.Fields{"Error": err}).
			Error("Revelo: Cannot unmount")
		return err
	}

	return nil
}

//
// FUSE implementation (check bazil-fuse for the usage)
// TODO: Make &FILE &DIR same across multiple access.
// TODO: Clean up following structs - remove unused.
type FS struct {
	Acc   *accio.Access
	RData *ReveloData

	remoteDir string
	cacheDir  string
}

type DIR struct {
	Acc   *accio.Access
	RData *ReveloData
	Entry horcrux.Entry

	remoteDir string
	cacheDir  string
}

type FILE struct {
	Acc   *accio.Access
	RData *ReveloData
	Entry horcrux.Entry

	remoteName string
	cacheName  string

	h *HANDLE
}

type HANDLE struct {
	Acc *accio.Access
	f   *FILE
}

// Updates Entry in dirTree: old -> new
func updateMetaEntry(data *ReveloData, old horcrux.Entry, new horcrux.Entry) error {

	data.lock.Lock()
	defer data.lock.Unlock()

	err := dirTree.Update(data.Root, old, new)
	if err != nil {
		log.WithFields(log.Fields{"old": old, "new": new}).Error("dirTree update failed")
		return err
	}

	log.WithFields(log.Fields{"old": old, "new": new}).Debug("dirTree update ok")
	return nil
}

// Saves Meta data from dirTree to meta File
func saveMeta(acc *accio.Access, data *ReveloData) error {
	var Meta *horcrux.Meta
	var err error

	data.lock.RLock()
	Meta, err = dirTree.GetMeta(data.Root)
	Meta.Config = data.Config
	Meta.CurrVer = data.CurrVer
	data.lock.RUnlock()

	if err != nil {
		log.Error("saveMeta: Cannot get Meta data")
		return err
	}

	js, err := json.MarshalIndent(Meta, "", "    ")
	if err != nil {
		log.WithFields(
			log.Fields{"Meta": Meta, "Error": err}).Error("saveMeta: Cannot marshal meta")
		return err
	}

	metaFile, err := os.OpenFile(data.cacheDir+"/"+data.metaName, os.O_WRONLY|os.O_SYNC, 0600)
	if err != nil {
		log.WithFields(log.Fields{"Meta File": data.metaName, "Error": err}).Error("saveMeta: Cannot open meta file")
		return err
	}
	defer metaFile.Close()

	// XXX Using File EX lock to avoid multiple access
	for err = syscall.Flock(int(metaFile.Fd()), syscall.LOCK_EX); err == syscall.EINTR; {
	}

	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("saveMeta: Cannot lock meta file")
		return err
	}

	defer syscall.Flock(int(metaFile.Fd()), syscall.LOCK_UN)

	metaFile.Truncate(0) //XXX: Do Truncate(n) after write succeeds!!
	n, err := metaFile.Write(js)
	if err != nil {
		log.WithFields(log.Fields{
			"Meta file": data.metaName, "Wrote": n, "Size": len(js), "Error": err,
		}).Error("saveMeta: Cannot write to meta file")
		return err
	}

	log.Debug("saveMeta: Done")
	return nil
}

//
// Handle Helper Functions
//

// Creates a new chunk - extends file
func createChunk(h *HANDLE, chunkIdx int64, buf []byte, off int64, sz int) (int, error) {
	var chFile *os.File
	var err error
	var wrote int

	cacheName := h.f.cacheName + "." + strconv.FormatInt(chunkIdx, 10)
	log.WithFields(log.Fields{
		"CacheName": cacheName,
		"ChunkIdx":  chunkIdx,
		"Offset":    off,
		"Size":      sz,
	}).Debug("Revelo::createChunk")

	err = os.MkdirAll(path.Dir(cacheName), 0700) //XXX revisit permission
	if err != nil {
		log.WithFields(log.Fields{
			"CacheName": cacheName,
			"Perm":      0700,
			"Error":     err,
		}).Error("Revelo::createChunk: Cannot MkdirAll")
		return 0, err
	}

	chFile, err = os.OpenFile(cacheName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		// Chunk exists - we should not be here
		log.WithFields(log.Fields{
			"CacheName": cacheName,
			"Size":      sz,
			"ChunkIdx":  chunkIdx,
			"Error":     err,
		}).Error("Revelo::createChunk: Open failed")
		return 0, err
	}
	defer chFile.Close()

	wrote, err = chFile.WriteAt(buf, off)
	if err != nil {
		log.WithFields(log.Fields{
			"ChunkName": cacheName,
			"Off":       off,
			"Size":      sz,
			"Wrote":     wrote,
			"Error":     err,
		}).Error("Revelo::createChunk: WriteAt failed")
		return 0, err
	}

	return wrote, nil
}

// Writes to an existing chunk
func writeChunk(h *HANDLE, chunkIdx int64, buf []byte, off int64, sz int) (int, error) {
	var chFile *os.File
	var err error
	var wrote int

	cacheName := h.f.cacheName + "." + strconv.FormatInt(chunkIdx, 10)
	_, err = os.Stat(cacheName)
	chunkPresent := ((err == nil) || !os.IsNotExist(err))

	log.WithFields(log.Fields{
		"CacheName": cacheName,
		"ChunkIdx":  chunkIdx,
		"Offset":    off,
		"Size":      sz,
		"Present":   chunkPresent,
		"Error":     err,
	}).Debug("Write: writeChunk")

	if chunkPresent == false {
		err = os.MkdirAll(path.Dir(cacheName), 0700) //XXX revisit permission
		if err != nil {
			log.WithFields(log.Fields{
				"CacheName": cacheName,
				"Perm":      0700,
				"Error":     err,
			}).Error("Revelo: Cannot MkdirAll")
			return 0, err
		}

		// Check if its partial write
		if sz < horcrux.CHUNKSIZE {
			// Get the chunk from remote
			remoteName := h.f.remoteName + "." + strconv.FormatInt(chunkIdx, 10)
			acc := *h.Acc
			err = acc.GetFile(remoteName, cacheName)
			if err != nil {
				log.Errorf("Revelo:writeChunk: Cannot get chunk %v for partial write, err %v", remoteName, err)
				return 0, err
			}

			chFile, err = os.OpenFile(cacheName, os.O_WRONLY, 0700) //XXX Revisit perm
		} else {
			//XXX Revisit perm
			chFile, err = os.OpenFile(cacheName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		}
	} else {
		chFile, err = os.OpenFile(cacheName, os.O_WRONLY, 0600) //XXX Revisit perm
	}

	if err != nil {
		log.WithFields(log.Fields{
			"CacheName":    cacheName,
			"Error":        err,
			"ChunkPresent": chunkPresent,
			"Size":         sz,
		}).Error("writeChunk: Open failed")
		return 0, err
	}
	defer chFile.Close()

	// Now we have the chunk or will be writing one
	wrote, err = chFile.WriteAt(buf, off)
	if err != nil {
		log.WithFields(log.Fields{
			"ChunkName": cacheName,
			"Off":       off,
			"Size":      sz,
			"Wrote":     wrote,
			"Error":     err,
		}).Error("writeChunk: WriteAt failed")
		return 0, err
	}

	return wrote, nil
}

// Write handler
func (h *HANDLE) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	var chunkIdx, offInChunk, numChunks int64

	f := h.f
	size := len(req.Data)
	resp.Size = -1

	// TODO: Use the masks!!
	chunkIdx = int64(req.Offset / horcrux.CHUNKSIZE)
	offInChunk = req.Offset - chunkIdx*horcrux.CHUNKSIZE
	newSize := req.Offset + int64(size)

	log.WithFields(log.Fields{
		"File":              f.cacheName,
		"chunkIdx":          chunkIdx,
		"offInChunk":        offInChunk,
		"numChunks in File": f.Entry.NumChunks,
		"Size":              size,
	}).Debug("Revelo: Write...")

	remain := size
	wrote := 0
	toWrite := horcrux.CHUNKSIZE

	for (remain > 0) && (chunkIdx < f.Entry.NumChunks) {
		if remain < toWrite {
			toWrite = remain
		}

		if offInChunk+int64(toWrite) > horcrux.CHUNKSIZE {
			// Spans more than one chunk
			toWrite -= int(offInChunk)
		}

		n, err := writeChunk(h, chunkIdx, req.Data[wrote:], offInChunk, toWrite)
		if err != nil || n == 0 {
			log.WithFields(log.Fields{
				"File":     f.cacheName,
				"chunkIdx": chunkIdx,
				"OffSet":   offInChunk,
				"Size":     toWrite,
				"Wrote":    wrote,
				"n":        n,
				"Err":      err,
			}).Error("Write: writeChunk failed or wrote 0")
			return err
		}

		offInChunk = 0
		toWrite = horcrux.CHUNKSIZE

		wrote += n
		remain -= n
		chunkIdx += 1
	}

	if remain == 0 {
		if newSize > f.Entry.Stat.Size {
			// File size extended within chunk range
			newEntry := f.Entry
			newEntry.Stat.Size = newSize
			if updateMetaEntry(f.RData, f.Entry, newEntry) != nil {
				log.WithFields(log.Fields{"OldEntry": f.Entry,
					"NewEntry": newEntry,
				}).Error("writeChunk: updateMetaEntry Failed")
			}
			f.Entry = newEntry

			err := saveMeta(f.Acc, f.RData)
			if err != nil {
				log.Error("writeChunk: cannot update meta for new size")
				return err
			}
		}

		resp.Size = wrote

		// Done writing
		log.WithFields(log.Fields{
			"File":    f.cacheName,
			"Offset":  req.Offset,
			"Size":    size,
			"Wrote":   wrote,
			"oldSize": f.Entry.Stat.Size,
			"newSize": newSize,
		}).Debug("Write: within chunk range done")

		return nil
	}

	// Extending file with new chunks
	toWrite = horcrux.CHUNKSIZE
	for remain > 0 {
		if remain < toWrite {
			toWrite = remain
		}

		n, err := createChunk(h, chunkIdx, req.Data[wrote:], offInChunk, toWrite)
		if err != nil {
			log.WithFields(log.Fields{
				"File":     f.cacheName,
				"Offset":   offInChunk,
				"Size":     toWrite,
				"chunkIdx": chunkIdx,
			}).Error("Write: createChunk failed")
			return err
		}

		offInChunk = 0

		wrote += n
		remain -= n
		chunkIdx++
	}

	// update meta data
	newSize = req.Offset + int64(size)
	numChunks = int64((newSize + horcrux.CHUNKSIZE - 1) / horcrux.CHUNKSIZE)
	if numChunks > f.Entry.NumChunks {
		log.WithFields(log.Fields{
			"OldSize":   f.Entry.Stat.Size,
			"NewSize":   newSize,
			"oldChunks": f.Entry.NumChunks,
			"newChunks": numChunks,
		}).Info("Write: Called to extend file")
		newEntry := f.Entry
		newEntry.NumChunks = numChunks
		newEntry.Stat.Size = newSize
		if updateMetaEntry(f.RData, f.Entry, newEntry) != nil {
			log.WithFields(log.Fields{"OldEntry": f.Entry,
				"NewEntry": newEntry,
			}).Error("writeChunk: updateMetaEntry Failed")
		}
		f.Entry = newEntry
	}

	err := saveMeta(f.Acc, f.RData)
	if err != nil {
		log.Error("writeChunk: cannot update meta for new chunks")
		return err
	}

	resp.Size = wrote
	return nil
}

// Reads from chunk
func readChunk(h *HANDLE, chunkIdx int64, buf []byte, off int64, sz int) (int, error) {

	remoteName := h.f.remoteName + "." + strconv.FormatInt(chunkIdx, 10)
	cacheName := h.f.cacheName + "." + strconv.FormatInt(chunkIdx, 10)

	log.WithFields(log.Fields{
		"CacheName":  cacheName,
		"RemoteName": remoteName,
		"ChunkIdx":   chunkIdx,
		"Size":       sz,
	}).Debug("readChunk")

	_, err := os.Stat(cacheName)
	chunkPresent := ((err == nil) || !os.IsNotExist(err))
	log.WithFields(log.Fields{
		"ChunkNAme":  cacheName,
		"Present":    chunkPresent,
		"Stat Error": err,
	}).Debug("readChunk: Testing for presence")

	if chunkPresent == false {
		if remoteName == "" {
			// Doesn't have a remote file, must be new local one
			// We shouldn't be calling read first
			log.WithFields(log.Fields{"cacheName": cacheName,
				"chunkIdx": chunkIdx,
				"Off":      off,
				"Size":     sz,
			}).Error("readChunk: cannot read - new local file without a remote... ")
			return 0, syscall.ENOENT
		}

		err := os.MkdirAll(path.Dir(cacheName), 0700) //XXX revisit permissions
		if err != nil {
			log.WithFields(log.Fields{
				"cacheName": cacheName,
				"Perm":      0700,
				"Error":     err,
			}).Error("Revelo: Cannot Mkdirall")
		}
		acc := *h.Acc
		err = acc.GetFile(remoteName, cacheName) //XXX Check for errors here
		if err != nil {
			return 0, err
		}
	}

	chFile, err := os.Open(cacheName)
	if err != nil {
		log.WithFields(log.Fields{
			"cacheName": cacheName,
			"Error":     err,
		}).Error("readChunk: Open failed")
		return 0, err
	}
	defer chFile.Close()

	// Sz can be less or more than CHUNKSIZE  //XXX Clean this up?
	if sz < horcrux.CHUNKSIZE {
		buf = buf[:sz]
	} else {
		buf = buf[:horcrux.CHUNKSIZE]
	}

	read, err := chFile.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		log.WithFields(log.Fields{
			"ChunkName": cacheName,
			"Off":       off,
			"Size":      sz,
			"Read":      read,
			"Error":     err,
		}).Error("readChunk Failed")
		return 0, err
	}

	buf = buf[:read]
	return read, nil
}

func (h *HANDLE) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	var chunkIdx, offInChunk, totalRead, numChunks int64

	f := h.f

	// TODO: Use the masks!!
	chunkIdx = int64(req.Offset / horcrux.CHUNKSIZE)
	offInChunk = req.Offset - chunkIdx*horcrux.CHUNKSIZE
	numChunks = int64((req.Size + horcrux.CHUNKSIZE - 1) / horcrux.CHUNKSIZE)

	if chunkIdx >= f.Entry.NumChunks || req.Size == 0 {
		resp.Data = []byte{}
		return nil
	}

	if chunkIdx+numChunks > f.Entry.NumChunks {
		numChunks = f.Entry.NumChunks - chunkIdx
	}

	resp.Data = make([]byte, numChunks*horcrux.CHUNKSIZE)
	remain := req.Size
	totalRead = 0
	for i := int64(0); i < numChunks; i++ {
		n, err := readChunk(h, chunkIdx+i, resp.Data[totalRead:], offInChunk, remain)
		if err != nil && err != io.EOF {
			log.WithFields(log.Fields{
				"File":     f.cacheName,
				"chunkIdx": chunkIdx,
				"Error":    err,
			}).Error("Read: readChunk failed")
			return err
		}

		offInChunk = 0

		remain -= n
		totalRead += int64(n)

		if err == io.EOF {
			break
		}

	}

	resp.Data = resp.Data[:totalRead]
	log.WithFields(log.Fields{
		"File":   f.cacheName,
		"Offset": req.Offset,
		"Size":   req.Size,
		"Read":   totalRead,
	}).Debug("Read")

	return nil
}

// Flush handler
func (h *HANDLE) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	// TODO: Flush all the modified chunks ???
	// For that we need to keep track of modified chunks - part of ver control?

	return nil
}

// Release handler
// TODO: Need this when we have &FILE same across multiple access
func (h *HANDLE) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	return nil
}

///////////////////////////
// File specific functions
///////////////////////////

func (f *FILE) Attr(ctx context.Context, a *fuse.Attr) error {
	stat := f.Entry.Stat
	a.Mode = stat.Mode
	a.Size = uint64(stat.Size)
	a.Uid = stat.Uid
	a.Gid = stat.Gid

	return nil
}

func (f *FILE) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {

	log.WithFields(log.Fields{
		"File":        f.Entry.Name,
		"Cache Name":  f.cacheName,
		"Remote Name": f.remoteName,
	}).Debug("Revelo: Open")

	// XXX TODO XXX XXX XXX
	// Fix this - need to preserve f, h across lookups, open
	// to handle the o_excl, etc.

	if f.h != nil {
		//XXX Cannot happen now!!
		log.WithFields(log.Fields{
			"File":   f.Entry.Name,
			"Handle": f.h,
		}).Warning("Revelo: Open - Handle not null, more than one openers")
		return f.h, nil
	}

	h := &HANDLE{Acc: f.Acc, f: f}
	f.h = h

	log.WithFields(log.Fields{
		"File":   f.Entry.Name,
		"Flags":  req.Flags,
		"Handle": f.h,
	}).Debug("Open: ")
	return h, nil
}

// Fsync handler
// XXX Sync the chunk files here??
func (f *FILE) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	return nil
}

//////////////////
// Dir functions
/////////////////

func (f FS) Root() (fs.Node, error) {
	var remoteDir string

	root := f.RData.Root.Entry
	if f.remoteDir == "" {
		remoteDir = f.RData.CurrVer + "/" + root.Name
	} else {
		remoteDir = f.remoteDir + "/" + f.RData.CurrVer + "/" + root.Name
	}

	return &DIR{
		Acc:       f.Acc,
		RData:     f.RData,
		Entry:     root,
		remoteDir: remoteDir,
		cacheDir:  f.cacheDir,
	}, nil
}

func (d *DIR) Attr(ctx context.Context, attr *fuse.Attr) error {
	stat := d.Entry.Stat
	attr.Mode = stat.Mode
	attr.Size = uint64(stat.Size)
	attr.Uid = stat.Uid
	attr.Gid = stat.Gid
	return nil
}

func (d *DIR) Lookup(ctx context.Context, Name string) (fs.Node, error) {

	var dirPrefix string
	if d.Entry.Prefix != "" {
		dirPrefix = d.Entry.Prefix + "/" + d.Entry.Name
	} else {
		dirPrefix = d.Entry.Name
	}

	d.RData.lock.RLock()
	defer d.RData.lock.RUnlock()

	log.WithFields(log.Fields{"Entry": Name, "Dir Prefix": dirPrefix}).Debug("dirTree Lookup: ")

	dirTreeNode, err := dirTree.Lookup(d.RData.Root, dirPrefix, Name)
	if err != nil {
		log.WithFields(log.Fields{"Name": Name, "Error": err}).Error("Lookup: dirTree lookup failed")
		return nil, fuse.ENOENT
	}

	entry := dirTreeNode.Entry

	if entry.IsDir {
		return &DIR{Acc: d.Acc,
			RData:     d.RData,
			Entry:     entry,
			remoteDir: d.remoteDir + "/" + Name,
			cacheDir:  d.cacheDir + "/" + Name}, nil
	}

	return &FILE{Acc: d.Acc,
		RData:      d.RData,
		Entry:      entry,
		remoteName: d.remoteDir + "/" + Name,
		cacheName:  d.cacheDir + "/" + Name,
		h:          nil}, nil
}

func (d *DIR) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {

	dirDirs := []fuse.Dirent{}

	log.WithFields(log.Fields{"Dir": d.Entry.Name, "Prefix": d.Entry.Prefix}).Debug("ReadDirAll: ")

	d.RData.lock.RLock()
	dirTreeNode, err := dirTree.Lookup(d.RData.Root, d.Entry.Prefix, d.Entry.Name)
	tmp := *dirTreeNode
	d.RData.lock.RUnlock()

	if err != nil {
		log.WithFields(log.Fields{"Name": d.Entry.Name, "Error": err}).Error("ReadDirAll: dirTree lookup failed")
		return nil, fuse.ENOENT
	}

	for i := 0; i < dirTree.NumKids(&tmp); i++ {
		k, _ := dirTree.GetKid(&tmp, i)
		ent := k.Entry
		t := fuse.DT_Unknown
		if ent.IsDir {
			t = fuse.DT_Dir
		} else {
			t = fuse.DT_File
		}
		dirDirs = append(dirDirs, fuse.Dirent{Inode: 0, Type: t, Name: ent.Name})
	}

	return dirDirs, nil
}

func (d *DIR) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {

	// TODO: XXX
	//
	// - Check for req.Create flags!!
	//	- No need to handle O_EXCL - lookup takes care of it.
	// Anything else???
	// 	- How to use the req.Umask?
	//
	// log.Debug("Revelo:: Create called")

	acc := d.Acc
	entry := d.Entry

	var prefix string
	if entry.Prefix == "" {
		prefix = entry.Name
	} else {
		prefix = entry.Prefix + "/" + entry.Name
	}

	stat := horcrux.Stat{Mode: req.Mode, Size: 0, Uid: entry.Stat.Uid, Gid: entry.Stat.Gid}
	newEntry := horcrux.Entry{Name: req.Name, Prefix: prefix, IsDir: false, Stat: stat, NumChunks: 0}

	d.RData.lock.Lock()
	err := dirTree.Insert(d.RData.Root, newEntry)
	d.RData.lock.Unlock()

	if err != nil {
		log.WithFields(log.Fields{"newEntry": newEntry}).Error("Cannot insert to dir tree")
		return nil, nil, err
	}

	if err := saveMeta(d.Acc, d.RData); err != nil {
		log.Error("Revelo::Create: cannot update meta for create new file")
		return nil, nil, err
	}

	f := &FILE{Acc: acc,
		RData:      d.RData,
		Entry:      newEntry,
		cacheName:  d.cacheDir + "/" + req.Name,
		remoteName: ""}

	h := &HANDLE{Acc: acc, f: f}
	f.h = h

	resp.LookupResponse.Attr = fuse.Attr{Mode: stat.Mode,
		Size: uint64(stat.Size),
		Uid:  stat.Uid,
		Gid:  stat.Gid}

	// XXX Populate resp.OpenResponse.Flags properly
	// resp.OpenResponse.Flags = ???

	//XXX check the return params with fuse
	return f, h, nil
}

func (d *DIR) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	dirPrefix := ""
	if d.Entry.Prefix != "" {
		dirPrefix = d.Entry.Prefix + "/" + d.Entry.Name
	}

	d.RData.lock.Lock()
	err := dirTree.Delete(d.RData.Root, dirPrefix, req.Name, req.Dir)
	d.RData.lock.Unlock()

	if err != nil {
		log.WithFields(log.Fields{"Prefix": dirPrefix, "Name": req.Name}).Error("Cannot Delete from dir tree")
		return err
	}

	if err := saveMeta(d.Acc, d.RData); err != nil {
		log.Error("Remove: cannot update meta for remove file")
		return err
	}

	//TODO:  Remove temp cache dir/files...
	return nil
}

func (d *DIR) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	var prefix string

	entry := d.Entry
	if entry.Prefix == "" {
		prefix = entry.Name
	} else {
		prefix = entry.Prefix + "/" + entry.Name
	}

	//XXX Revisit size value - 4k for now.
	//XXX Should we use local Uid, Gid?
	stat := horcrux.Stat{Mode: req.Mode, Size: 4096, Uid: entry.Stat.Uid, Gid: entry.Stat.Gid}
	newEntry := horcrux.Entry{
		Name:      req.Name,
		Prefix:    prefix,
		IsDir:     true,
		Stat:      stat,
		NumChunks: 0}

	d.RData.lock.Lock()
	err := dirTree.Insert(d.RData.Root, newEntry)
	d.RData.lock.Unlock()

	if err != nil {
		log.WithFields(log.Fields{"newEntry": newEntry, "Error": err}).Error("Mkdir: Cannot insert new entry")
		return nil, err
	}

	if err := saveMeta(d.Acc, d.RData); err != nil {
		log.Error("Mkdir: save Meta  failed")
		// XXX May be later save meta will work... either way, it will recover after restart
		// d.RData.lock.Lock()
		// dirTree.Delete(d.RData.Root, prefix, req.Name)
		// d.RData.lock.Unlock()
		return nil, err
	}

	newD := &DIR{Acc: d.Acc,
		RData:     d.RData,
		Entry:     newEntry,
		cacheDir:  d.cacheDir + "/" + req.Name,
		remoteDir: ""}

	return newD, nil
}

//
// XXX Should we handle Rename ???
// If yes, how do we treat the files - as new or just the old ones?
// Just the old ones is not good. Cannot get from remote as names changed and
// we are not keeping track of remote names separately. We might, if we need this functionality
//
/* COMMENTED OUT FOR NOW!!
func (d *DIR) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	var oldPrefix, newPrefix string

	nd, ok := newDir.(*DIR)
	if !ok {
		log.WithFields(log.Fields{"newDir": newDir}).Error("Rename: New Dir is not a DIR")
		return syscall.EINVAL	// Should we fix fuse.error ???
	}

	if d.Entry.Prefix != "" {
		oldPrefix = d.Entry.Prefix + "/" + d.Entry.Name
	} else {
		oldPrefix = d.Entry.Name
	}

	if nd.Entry.Prefix != "" {
		newPrefix = nd.Entry.Prefix + "/" + nd.Entry.Name
	} else {
		newPrefix = nd.Entry.Name
	}

	log.WithFields(log.Fields{"Dir": d, "newDir": nd, "Request": req,
			  "Old Prefix": oldPrefix, "New Prefix": newPrefix,
		}).Error("Rename request")

	//XXX
	//XXX Do the locking properly, when we do support rename

	d.RData.lock.Lock()

	foundDir := false
	idx := -1
	for i, ent := range d.RData.Meta.Entries {
		if ent.Prefix == oldPrefix && ent.Name == req.OldName {
			if ent.IsDir == false {
				d.RData.Meta.Entries[i].Prefix = newPrefix
				d.RData.Meta.Entries[i].Name = req.NewName
				d.Entry = d.RData.Meta.Entries[i]

				d.RData.lock.Unlock()

				//XXX We are not verifying the New Dir to be part of Meta.Entries now..
				//XXX Is it even possible? I guess not as it will be lookuped up before
				//XXX this is called...
				if err := saveMeta(d.Acc, d.RData); err != nil {
					log.Error("Rename file: cannot save Meta")
					return err
				}

				return nil
			} else {
				foundDir = true
				idx = i
				break
			}
		}
	}

	if !foundDir {
		d.RData.lock.Unlock()
		return syscall.ENOENT
	}

	// Rename a dir
	d.RData.Meta.Entries[idx].Prefix = newPrefix
	d.RData.Meta.Entries[idx].Name = req.NewName
	d.Entry = d.RData.Meta.Entries[idx]

	oldPrefix = oldPrefix + "/" + req.OldName
	newPrefix = newPrefix + "/" + req.NewName

	for i, e2 := range d.RData.Meta.Entries {
		if e2.Prefix == oldPrefix {
			d.RData.Meta.Entries[i].Prefix = newPrefix
		}
	}

	d.RData.lock.Unlock()

	if err := saveMeta(d.Acc, d.RData); err != nil {
		log.Error("Rename dir: cannot save Meta")
		return err
	}

	return nil
}
*/

// XXX This is duplicate code...
// TODO: Give credit to bazil.org/fuse or whoever wrote this originally
// TODO: Put these duplicated code in utils package?
func fileMode(unixMode uint32) os.FileMode {
	mode := os.FileMode(unixMode & 0777)
	switch unixMode & syscall.S_IFMT {
	case syscall.S_IFREG:
		// nothing
	case syscall.S_IFDIR:
		mode |= os.ModeDir
	case syscall.S_IFCHR:
		mode |= os.ModeCharDevice | os.ModeDevice
	case syscall.S_IFBLK:
		mode |= os.ModeDevice
	case syscall.S_IFIFO:
		mode |= os.ModeNamedPipe
	case syscall.S_IFLNK:
		mode |= os.ModeSymlink
	case syscall.S_IFSOCK:
		mode |= os.ModeSocket
	default:
		// no idea
		mode |= os.ModeDevice
	}
	if unixMode&syscall.S_ISUID != 0 {
		mode |= os.ModeSetuid
	}
	if unixMode&syscall.S_ISGID != 0 {
		mode |= os.ModeSetgid
	}
	return mode
}
