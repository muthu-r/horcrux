//
// Many thanks to
// 	(1): https://blogs.oracle.com/janp/entry/how_the_scp_protocol_works
// and
// 	(2): https://gist.github.com/jedy/3357393 for pointing to (1)
//
// TODO:
//	- VErify ctrl message and use its len ???
//
package scp

import (
	"bytes"
	"fmt"
    "net"
	"golang.org/x/crypto/ssh"
	"os"
	"os/user"
	"strings"
	"syscall"

	log "github.com/Sirupsen/logrus"
)

type Data struct {
	ScpArgs  string
	CacheDir string

	User       string
	Passwd     string
	Host       string
	RemotePath string

	client *ssh.Client
}

const RSA_PRIVATEKEY_FILE = ".ssh/id_rsa"
const SCP_COMMAND = "/usr/bin/scp -vf "

func (D *Data) String() string {
	return "SCP::" + "User:" + D.User + ",Host:" + D.Host + ",RemotePath:" + D.RemotePath
}

func (D *Data) Name() string {
	return "SCP"
}

func parse(D *Data) error {
	args := D.ScpArgs

	Idx := strings.Index(args, "::")
	if Idx != -1 {
		/* Password given */
		D.User = args[:Idx]
		args = args[Idx+2:] // for "::"
		Idx = strings.Index(args, "@")
		if Idx == 0 || Idx == -1 {
			return syscall.EINVAL
		}
		D.Passwd = args[:Idx]
		args = args[Idx+1:]
	} else {
		Idx = strings.Index(args, "@")
		if Idx == 0 || Idx == -1 {
			return syscall.EINVAL
		}
		D.User = args[:Idx]
		args = args[Idx+1:]
	}

	/* Get Host */
	Idx = strings.Index(args, ":")
	if Idx == -1 || Idx == 0 {
		return syscall.EINVAL
	}
	D.Host = args[:Idx]
	args = args[Idx+1:]

	/* Get remote path */
	D.RemotePath = args
	return nil
}

func (D *Data) Init() (string, error) {
	var auth []ssh.AuthMethod

	err := parse(D)
	if err != nil {
		return "", err
	}

	log.WithFields(
		log.Fields{
			"SCP Arguments": D.ScpArgs,
			"Cache Dir":     D.CacheDir,
			"User":          D.User,
			"Passwd":        D.Passwd, //XXX Remove this
			"Host":          D.Host,
			"Remote Path":   D.RemotePath,
		}).Info("Accio - SCP")

	usr, err := user.Current()
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Error("SCP: Cannot get usr info")
		return "", err
	}

	if D.Passwd == "" {
		privateKey := make([]byte, 4096) //XXX 4K should be enough!!

		idFile, err := os.Open(usr.HomeDir + "/" + RSA_PRIVATEKEY_FILE)
		if err != nil {
			log.WithFields(
				log.Fields{
					"File":  usr.HomeDir + "/" + RSA_PRIVATEKEY_FILE,
					"Error": err,
				}).Error("SCP: Cannot open KEY file")
			return "", err
		}
		defer idFile.Close()

		n, err := idFile.Read(privateKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("SCP: Cannot read KEY file")
			return "", err
		}

		privateKey = privateKey[:n]
		sig, err := ssh.ParsePrivateKey(privateKey)
		if err != nil {
			log.WithFields(log.Fields{"Error": err}).Error("SCP: Cannot parse KEY")
			return "", err
		}

		auth = []ssh.AuthMethod{ssh.PublicKeys(sig)}
	} else {
		auth = []ssh.AuthMethod{ssh.Password(D.Passwd)}
	}

    hostKeyCB := func(hostname string,
                        remote net.Addr,
                        key ssh.PublicKey) error {
                  return nil
                }

	cfg := &ssh.ClientConfig{User: usr.Username, Auth: auth, HostKeyCallback: hostKeyCB}

	client, err := ssh.Dial("tcp", D.Host+":22", cfg)
	if err != nil {
		log.WithFields(
			log.Fields{"Data": D,
				"Error": err,
			}).Error("SCP: Cannot connect to remote host")
		return "", err
	}

	D.client = client

	return D.RemotePath, nil
}

func (D *Data) GetFile(src string, dst string) error {
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	sess, err := D.client.NewSession()
	if err != nil {
		log.WithFields(
			log.Fields{"SCP Data": D, "Err": err}).Error("SCP: Cannot create new session")
		return err
	}
	defer sess.Close()

	sess.Stdout = &stdout
	sess.Stderr = &stderr

	go func() {
		wrPipe, _ := sess.StdinPipe()
		defer wrPipe.Close()

		fmt.Fprintf(wrPipe, "\x00")
		fmt.Fprintf(wrPipe, "\x00")
		fmt.Fprintf(wrPipe, "\x00")
		fmt.Fprintf(wrPipe, "\x00")
	}()

	f, err := os.Create(dst)
	if err != nil {
		log.WithFields(
			log.Fields{"DST": dst, "Err": err}).Error("SCP: Cannot create local cache")
		return err
	}

	defer f.Close()

	if err := sess.Run(SCP_COMMAND + src); err != nil {
		log.WithFields(log.Fields{
			"CMD":   SCP_COMMAND + src,
			"Error": err,
		}).Error("SCP: Cannot run scp command")

		return err
	}

	idx := strings.Index(stdout.String(), "\n")
	if idx == -1 {
		log.Error("SCP: Response doesnt have new line")
		return syscall.EINVAL
	}

	ctrl := stdout.String()[:idx]
	// XXX VErify ctrl message and use its len ???
	log.Info(ctrl)
	l := len(stdout.String())
	f.Write([]byte(stdout.String()[idx+1 : l-1])) // l-1 to remove trailing \x00 ack

	return nil

}
