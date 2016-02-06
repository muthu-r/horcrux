//
// Implements AWS S3 access interface
//
package s3

import (
	"os"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	log "github.com/Sirupsen/logrus"
)

type Data struct {
	S3Args  string
	CacheDir string

	BktName string
	Region  string

	s3Sess *session.Session
}

func (D Data) String() string {
	return "S3:: " + " Bucket: " + D.BktName + " Region: " + D.Region + " CacheDir: " + D.CacheDir
}

func (D *Data) Name() string {
	return "S3"
}

func parse(D *Data) error {
	args := D.S3Args

	Idx := strings.Index(args, "@")
	if Idx == 0 || Idx == -1 {
		return syscall.EINVAL
	}
	D.BktName = args[:Idx]
	D.Region = args[Idx+1:]

	return nil
}

func (D *Data) Init() (string, error) {
	err := parse(D)
	if err != nil {
		return "", err
	}

	log.WithFields(
		log.Fields{
			"S3 Args":  D.S3Args,
			"Cache Dir": D.CacheDir,
			"Bucket":    D.BktName,
			"Region":    D.Region,
		}).Info("Accio - S3")

	D.s3Sess = session.New(&aws.Config{Region: aws.String(D.Region)})

	return "", nil
}

func (D *Data) GetFile(src string, dst string) error {
	log.WithFields(
		log.Fields{"src": src,
			"dst":      dst,
			"S3 Data": D,
		}).Info("S3: GetFile")

	file, err := os.Create(dst)
	if err != nil {
		log.WithFields(log.Fields{"dst": dst}).Error("S3: Cannot create file")
		return err
	}
	defer file.Close()

	s3Param := &s3.GetObjectInput{
		Bucket: aws.String(D.BktName),
		Key:    aws.String(src)}
	downloader := s3manager.NewDownloader(D.s3Sess)
	n, err := downloader.Download(file, s3Param)
	if err != nil {
		log.WithFields(
			log.Fields{"S3": D, "Key": src, "Error": err}).Error("S3: Cannot download")
		os.Remove(dst)
		return err
	}

	log.WithFields(
		log.Fields{"S3": D, "Src": src, "Dst": dst, "Bytes recvd": n}).Debug("S3: GetFile")
	return nil
}
