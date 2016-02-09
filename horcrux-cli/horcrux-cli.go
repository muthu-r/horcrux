/*
 * TODO:
 *	- SetAttr
 *	- Rename
 *	- Fix "du" ?
 *	- clean-up cache dirs for removed dirs/files
 */
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"syscall"

	//	"github.com/codegangsta/cli"
	log "github.com/Sirupsen/logrus"

	"github.com/muthu-r/horcrux"
	"github.com/muthu-r/horcrux/reducto"
	"github.com/muthu-r/horcrux/revelo"
)

func Usage() {
	fmt.Printf("Usage: horcrux-cli generate <name> <in-dir> <out-dir>\n" +
		   "       horcrux-cli mount <name> <access-type> <mntdir>\n")
	return
}

const (
	WORKDIR = ".horcrux"
)

func createWorkDirs(name string) (string, error) {
	usr, err := user.Current()
	if err != nil {
		log.Errorf("Cannot get user info - error %v", err)
		return "", err
	}

	cd := usr.HomeDir + "/" + WORKDIR + "/" + name
	err = os.MkdirAll(cd, 0700)
	if err != nil {
		log.Errorf("Cannot create cachedir dir " + cd)
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

func main() {
	log.SetLevel(horcrux.LOGLEVEL)

	flag.Parse()
	if flag.NArg() < 1 {
		Usage()
		os.Exit(1)
	}

	if os.Args[1][:1] == "g" {
		if flag.NArg() != 4 {
			log.Error("Generate: Insufficient arguments")
			reducto.Usage()
			os.Exit(1)
		}
		err := reducto.Reducto(horcrux.CHUNK_TYPE_STATIC, os.Args[2], os.Args[3], os.Args[4])
		if err != nil {
			log.WithFields(log.Fields{
				"Name":    os.Args[2],
				"In Dir":  os.Args[3],
				"Out Dir": os.Args[4],
				"Error":   err,
			}).Error("Horcrux: Cannot generate")
			return
		}

		log.WithFields(log.Fields{
			"Name":    os.Args[2],
			"In Dir":  os.Args[3],
			"Out Dir": os.Args[4],
		}).Info("Horcrux: generated")
		return
	}

	if os.Args[1][:1] == "m" {
		if flag.NArg() != 4 {
			log.Error("Mount: Insufficient arguments")
			revelo.Usage()
			os.Exit(1)
		}

		horName := os.Args[2]
		accessArgs := os.Args[3]
		mntDir := os.Args[4]

		handleSignals(mntDir)

		cacheDir, err := createWorkDirs(horName)

		err = revelo.Revelo(horName, accessArgs, cacheDir, mntDir)
		if err != nil {
			log.Errorf("Cannot mount - error %v", err)
		}
		return
	}

	log.Error("Horcrux: Invalid Command")
	Usage()
}
