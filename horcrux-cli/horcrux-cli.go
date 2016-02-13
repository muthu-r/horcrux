/*
 * TODO:
 *	- SetAttr
 *	- Rename
 *	- Fix "du" ?
 *	- clean-up cache dirs for removed dirs/files
 */
package main

import (
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/codegangsta/cli"
	log "github.com/Sirupsen/logrus"

	"github.com/muthu-r/horcrux"
	"github.com/muthu-r/horcrux/reducto"
	"github.com/muthu-r/horcrux/revelo"
)

const (
	WORKDIR = ".horcrux"
)

func createWorkDirs(name string) (string, error) {
	usr, err := user.Current()
	if err != nil {
		fmt.Printf("Cannot get user info - error %v\n", err)
		return "", err
	}

	cd := usr.HomeDir + "/" + WORKDIR + "/" + name
	err = os.MkdirAll(cd, 0700)
	if err != nil {
		fmt.Printf("Cannot create cachedir dir %v\n", cd)
		return "", err
	}

	return cd, nil
}

func handleSignals(mntDir string) {
	sigConn := make(chan os.Signal, 10)
	signal.Notify(sigConn, os.Interrupt)
	signal.Notify(sigConn, syscall.SIGTERM)
	signal.Notify(sigConn, syscall.SIGUSR1)
	signal.Notify(sigConn, syscall.SIGUSR2)
	go func() {
		for {
			sig := <-sigConn
			if sig == syscall.SIGUSR1 {
				old := log.GetLevel()
				new := old + 1
				if new > log.DebugLevel {
					new = log.DebugLevel
				}
				if new != old {
					log.SetLevel(new)
					log.WithFields(log.Fields{"old": old, "new": new}).Error("Increased logging")
				}
			} else if sig == syscall.SIGUSR2 {
				old := log.GetLevel()
				new := old - 1
				if new < log.FatalLevel {
					new = log.FatalLevel
				}
				if new != old {
					log.WithFields(log.Fields{"old": old, "new": new}).Error("Decreased logging")
					log.SetLevel(new)
				}
			} else {
				log.Error("Interrupted... exiting")
				log.Infof("Unmounting %s ... ", mntDir)
				revelo.Unmount(mntDir)
				os.Exit(1)
			}
		}
	}()
}

func getChunkSize(chunksz string) int {
	var val int
	var err error

	shift := uint(0)
	len := len(chunksz)
	switch chunksz[len - 1] {
	case 'k', 'K':
		shift = 10
	        val, err = strconv.Atoi(chunksz[:len - 1])
	case 'm', 'M':
		shift = 20
	        val, err  = strconv.Atoi(chunksz[:len - 1])
	case 'g', 'G':
		shift = 30
		val, err  = strconv.Atoi(chunksz[:len - 1])
	default:
		shift = 0
		val, err = strconv.Atoi(chunksz)
	}

	if err != nil {
		fmt.Printf("Invalid chunk size %v, using default value %v\n", chunksz, horcrux.CHUNKSIZE_DEFAULT)
		return horcrux.CHUNKSIZE_DEFAULT
	}

	chunkSz := val << shift
	if chunkSz < (horcrux.CHUNKSIZE_MIN) {
		fmt.Printf("Chunk Size %v, less than minimum %v, using min size %v\n",
			chunkSz, horcrux.CHUNKSIZE_MIN, horcrux.CHUNKSIZE_MIN)
		return horcrux.CHUNKSIZE_DEFAULT
	}

	if (chunkSz & (chunkSz - 1)) != 0 {
		fmt.Printf("Chunk size %v not a power of 2, using default size %v\n",
			chunkSz, horcrux.CHUNKSIZE_DEFAULT)
		return horcrux.CHUNKSIZE_DEFAULT
	}

	return chunkSz
}

func generate(c *cli.Context) {
	if len(c.Args()) != 3 {
		fmt.Printf("Generate: Invalid arguments\n")
		cli.ShowSubcommandHelp(c)
		return
	}
	chunkSize := getChunkSize(chunksz)
	fmt.Printf("Generate: chunk sz %v\n", chunkSize)

	horName := c.Args()[0]
	inPath := c.Args()[1]
	outPath := c.Args()[2]

	err := reducto.Reducto(horcrux.CHUNK_TYPE_STATIC, chunkSize, horName, inPath, outPath)
	if err != nil {
		fmt.Printf("Generate failed: err = %v\n", err)
		return
	}

	fmt.Printf("Generate done... files in %v\n", outPath)
	return
}

func mount(c *cli.Context) {
	if len(c.Args()) != 3 {
		fmt.Printf("Mount: Invalid arguments\n")
		cli.ShowSubcommandHelp(c)
		return
	}

	horName := c.Args()[0]
	accessArgs := c.Args()[1]
	mntDir := c.Args()[2]

	handleSignals(mntDir)

	cacheDir, err := createWorkDirs(horName)
	err = revelo.Revelo(horName, accessArgs, cacheDir, mntDir)
	if err != nil {
		log.Errorf("Cannot mount - err: %v\n", err)
		return
	}

	return
}

var chunksz string
var horCmds = []cli.Command {
	{
		Name:	"generate",
		Aliases: []string{"g", "gen"},
		Usage:	"[options] <name> <in-dir> <out-dir>",
		Action: generate,
		Flags: []cli.Flag {
			cli.StringFlag {
				Name: "chunksize, s",
				Value: horcrux.CHUNKSIZE_DEFAULT_STR,
				Usage: "Chunk Size",
				Destination: &chunksz,
			},
		},
	},
	{
		Name:	"mount",
		Aliases: []string{"m", "mnt"},
		Usage: "<name> <access-type> <mnt-dir>\n" +
		       "   access-type is one of:\n" +
                       "       cp://full-path\n" +
                       "       scp://user::passwd@host:full-path (skip passwd, if auto-login is configured)\n" +
                       "       s3://bucket@region (credentials in ~/.aws/credentials)\n" +
                       "       minio://host:port/bucket (credentials in ~/.minio/horcrux.json)\n",
		Action: mount,
	},
}

func main() {
	log.SetLevel(horcrux.LOGLEVEL)
	
	app := cli.NewApp()
	app.Name = "horcrux-cli"
	app.Usage =  "Horcrux CLI"
	app.Version = horcrux.VERSION
	app.Commands = horCmds
	app.Run(os.Args)
}
