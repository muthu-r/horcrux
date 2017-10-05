//
// Global definitions
//
package horcrux

import (
	"os"

	log "github.com/Sirupsen/logrus"
)

const LOGLEVEL = log.InfoLevel

const (
	VERMAJOR = "00"
	VERMINOR = "03"
	VEREXTRA = "-rc2"
	VERSION  = VERMAJOR + "." + VERMINOR + VEREXTRA

	MINVER   = 1
	MAXVER   = 1000
	STARTVER = MINVER

	CHUNKSIZE_MIN         = (1 << 20) // 1M
	CHUNKSIZE_DEFAULT     = (64 << 20) // 64M
	CHUNKSIZE_DEFAULT_STR = "64M"
)

const (
	CHUNK_TYPE_STATIC = 1 + iota
	CHUNK_TYPE_ROLLSUM
)

type Config struct {
	Version   string `json:"Version"`
	ChunkType int    `json:"Chunk Type"`
	ChunkSize int    `json:"Chunk Size"`
}

type Stat struct {
	Mode os.FileMode `json:"Mode"`
	Size int64       `json:"Size"`
	Uid  uint32      `json:"Uid"` //XXX Get from running pid?
	Gid  uint32      `json:"Gid"` //XXX Get from running pid?
	/* TODO: Do we need {A,M,C}tim */
}

type Entry struct {
	Name      string `json:"Name"`
	Prefix    string `json:"Prefix"`
	IsDir     bool   `json:"IsDir"`
	Stat      Stat   `json:"Stat"`
	NumChunks int64  `json:"Number of Chunks"`
}

type Meta struct {
	Config   Config  `json:"Config"`
	CurrVer  string  `json:"Current Version"`
	NumFiles int     `json:"Num Files"`
	Entries  []Entry `json:"Entry List"`
}
