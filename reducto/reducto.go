//
// Reducto <in-dir> <out-dir>
//  - Converts files in <in-dir> to horcrux format and puts them 
//    in <out-dir>. <out-dir> can then be put in remote location (ex:aws s3, minio, scp, etc..) 
//    to use with on-demand local access and version control.
//

package reducto

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path"
	"strconv"
	"syscall"

	"github.com/muthur/horcrux"

	log "github.com/Sirupsen/logrus"
)

var Usage = func() {
	fmt.Printf("Usage: reducto <name> <in-dir> <out-dir>\n")
}

// Splits a file into multiple chunks - returns number of chunks
func split(Type int, inName string, outName string) (int64, error) {

	inFile, err := os.OpenFile(inName, os.O_RDONLY, 0)
	if err != nil {
		log.WithFields(log.Fields{"File": inName, "Error": err}).Error("Reducto: split - cannot open")
		return -1, err
	}
	defer inFile.Close()

	fi, err := inFile.Stat()
	if err != nil {
		log.WithFields(log.Fields{"File": inName, "Error": err}).Error("Reducto: Cannot stat file")
		return -1, err
	}

	numChunks := int64((fi.Size() + horcrux.CHUNKSIZE - 1) / horcrux.CHUNKSIZE)
	log.WithFields(log.Fields{"File": inName, "Size": fi.Size(), "NumChunks": numChunks}).Debug("Reducto: splitting")

	data := make([]byte, horcrux.CHUNKSIZE)

	for chunkIdx := int64(0); chunkIdx < numChunks; chunkIdx++ {
		chunkName := outName + "." + strconv.FormatInt(chunkIdx, 10)
		// TODO: See if we can pipe (or splice :))
		n, err := inFile.Read(data)
		if err != nil {
			log.WithFields(log.Fields{
				"In File":  inName,
				"Chunk":    chunkName,
				"chunkIdx": chunkIdx,
				"Error":    err,
			}).Error("Reducto: split - read failed")
			return -1, err
		}

		if n == 0 {
			// TODO : Can this happen? And is it error?
			continue
		}
		data = data[:n]

		chunkFile, err := os.Create(chunkName)
		if err != nil {
			log.WithFields(log.Fields{"In File": inName,
				"Chunk Index": chunkIdx,
				"Chunk Name":  chunkName,
				"Error":       err,
			}).Error("Reducto: split - cannot create chunk file")
			return -1, err
		}

		n2, err := chunkFile.Write(data)
		chunkFile.Close()

		if err != nil || n2 != n {
			log.WithFields(log.Fields{"File": inName,
				"Chunk":     chunkName,
				"Chunk Idx": chunkIdx,
				"n":         n,
				"n2":        n2,
				"Error":     err,
			}).Error("Reducto: read (n), wrote (n2): Failed")
			return -1, err
		}
	}

	return numChunks, nil
}

func Reducto(Type int, Name, inPath string, outPath string) error {

	inPath = path.Clean(inPath)
	outPath = path.Clean(outPath)

	log.WithFields(log.Fields{
		"Version":  horcrux.VERSION,
		"Type":     Type,
		"In File":  inPath,
		"Out File": outPath,
	}).Debug("Reducto")

	metaFile, err := os.Create(outPath + "/" + Name + ".meta")
	if err != nil {
		log.WithFields(log.Fields{
			"Meta File": outPath + "/" + Name + ".meta",
			"Error":     err,
		}).Error("Reducto: Cannot create Meta file")
		return err
	}
	defer metaFile.Close()

	Config := horcrux.Config{Version: horcrux.VERSION, ChunkType: Type, ChunkSize: horcrux.CHUNKSIZE}

	EntryList := []horcrux.Entry{}
	stat, err := getStat(inPath)
	if err != nil {
		log.WithFields(log.Fields{"In File": inPath, "Error": err}).Error("Reducto: Cannot stat in file")
		return err
	}

	Meta := &horcrux.Meta{}
	numFiles := 1
	prefix := ""
	currVer := "v" + strconv.Itoa(horcrux.STARTVER)

	if stat.Mode.IsDir() == false {
		log.Errorf("Reducto: input %v has to be a directory", inPath)
		return syscall.EINVAL
	}

	inBase := path.Base(inPath)
	inDir := path.Dir(inPath)

	perm := stat.Mode.Perm()
	os.Mkdir(outPath, perm)

	outPath = outPath + "/" + currVer
	os.MkdirAll(outPath+"/"+inBase, perm)

	root := horcrux.Entry{Name: inBase,
		Prefix:    prefix,
		IsDir:     true,
		Stat:      stat,
		NumChunks: 1}
	EntryList = append(EntryList, root)
	dirList := []string{inBase}

	for len(dirList) > 0 {
		dir := dirList[0]
		dirList = dirList[1:]
		file, err := os.Open(inDir + "/" + dir)
		if err != nil {
			log.WithFields(log.Fields{
				"inBase": inBase,
				"Dir":    inDir + "/" + dir,
				"Error":  err,
			}).Error("Reducto: Cannot Open - Avoid trailing slash for now")
			return err
		}

		log.WithFields(log.Fields{
			"inBase": inBase,
			"Dir":    inDir + "/" + dir,
		}).Debug("Reduto: Processing")

		dirEnts, err := file.Readdirnames(0)
		file.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"Dir":   inDir + "/" + dir,
				"Error": err,
			}).Error("Reducto: Cannot Readdirname")
			return err

		}

		for len(dirEnts) > 0 {
			ent := dirEnts[0]
			path := inDir + "/" + dir + "/" + ent
			dirEnts = dirEnts[1:]

			stat, err := getStat(path)
			if err != nil {
				log.WithFields(log.Fields{
					"Dir":   path,
					"Error": err,
				}).Error("Reducto: Cannot get stat")
				return err
			}

			isDir := stat.Mode.IsDir()

			var numChunks int64
			if isDir {
				perm := stat.Mode.Perm()
				err := os.Mkdir(outPath+"/"+dir+"/"+ent, perm)
				if err != nil {
					log.WithFields(log.Fields{
						"Dir":   outPath + "/" + dir + "/" + ent,
						"Perm":  perm,
						"Error": err,
					}).Error("Reducto: Cannot Mkdir")
					return err
				}
				dirList = append(dirList, dir+"/"+ent)
				numChunks = 1	//XXX Should we make this 0?
			} else {
				numChunks, _ = split(Type, path, outPath+"/"+dir+"/"+ent)
			}

			EntryList = append(EntryList, horcrux.Entry{Name: ent,
						Prefix:    dir,
						IsDir:     isDir,
						Stat:      stat,
						NumChunks: numChunks})
			numFiles += 1
		}
	}

	Meta.Config = Config
	Meta.CurrVer = currVer
	Meta.NumFiles = numFiles
	Meta.Entries = EntryList

	js, err := json.MarshalIndent(Meta, "", "    ")
	if err != nil {
		log.WithFields(log.Fields{"Meta": Meta, "Error": err}).Error("Reducto: Cannot marshal metadata")
		return err
	}

	n, err := metaFile.Write(js)
	if err != nil {
		log.WithFields(log.Fields{
			"Meta file": outPath + Name + ".meta",
			"Wrote":     n, "Size": len(js),
			"Error": err,
		}).Error("Reducto: Cannot write to meta file")
		return err
	}

	return nil
}

func getStat(name string) (horcrux.Stat, error) {
	ustat := new(unix.Stat_t)
	err := unix.Stat(name, ustat)
	if err != nil {
		log.WithFields(log.Fields{"File": name, "Stat": ustat, "Error": err}).Error("Reducto: getStat -  unix.Stat failed")
		return horcrux.Stat{}, err
	}

	mode := fileMode(ustat.Mode)
	stat := horcrux.Stat{Mode: mode, Uid: ustat.Uid, Gid: ustat.Gid, Size: ustat.Size}
	return stat, nil
}

// TODO: Give credit to bazil.org/fuse or whoever wrote this originally
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
