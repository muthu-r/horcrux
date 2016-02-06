// About Minio:
//   Minio is a distributed object storage server written in Golang.
//   Minio server, client and SDK are API compatible with Amazon S3 cloud storage service.
//   Source is available under free software / open source Apache license 2.0.
// Please refer to http://www.minio.io for more info.

package minio

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"syscall"
	"os/user"

	"github.com/minio/minio-go"

	log "github.com/Sirupsen/logrus"
)

const (
	MINIO_CONFIG_FILE = ".minio/horcrux.json" // ~/.minio/horcrux.json
)

var minioKey struct {
	AccessKeyId string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
}

type Data struct {
	MinioArgs string
	Endpoint string
	BktName  string
	CacheDir string
	s3Client minio.CloudStorageAPI
}

func (D *Data) String() string {
	return "MINIO:: " + " Endpoint: " + D.Endpoint + " Bucket: " + D.BktName + " CacheDir: " + D.CacheDir
}

func (D *Data) Name() string {
	return "MINIO"
}

func (D *Data) Init() (string, error) {

	usr, _ := user.Current()
	keyFile := usr.HomeDir + "/" + MINIO_CONFIG_FILE

	kf, err := os.Open(keyFile)
	if err != nil {
		log.Errorf("MINIO: Cannot open key file %v, err %v", keyFile, err)
		return "", err
	}
	defer kf.Close()
	keyData := make([]byte, 512)
	n, err := kf.Read(keyData)
	if err != nil || n == 0 {
		log.Errorf("MINIO: Cannot read key file, read %v, err %v", n, err)
		return "", err
	}
	keyData = keyData[:n]

	err = json.Unmarshal(keyData, &minioKey)
	if err != nil {
		log.Errorf("MINIO: Cannot unmarshal key data %v, err %v", string(keyData), err)
		return "", err
	}
	
	idx := strings.Index(D.MinioArgs, "/")
	if idx == -1 {
		return "", syscall.EINVAL
	}

	// minio => http, minios => https?
	D.Endpoint = "http://" + D.MinioArgs[:idx]
	D.BktName = D.MinioArgs[idx+1:]

	log.WithFields(log.Fields{
			"MinioArgs": D.MinioArgs,
			"Endpoint":   D.Endpoint,
			"Bucket Name": D.BktName,
			"Cache Dir": D.CacheDir,
		}).Info("Accio - MINIO")

	config := minio.Config{
			Endpoint: D.Endpoint,
			AccessKeyID: minioKey.AccessKeyId,
			SecretAccessKey: minioKey.SecretAccessKey}

	D.s3Client,_ = minio.New(config)
	return "", nil
}

func (D *Data) GetFile(src string, dst string) error {
	log.WithFields(log.Fields{
			"Endpoint":   D.Endpoint,
			"Bucket Name": D.BktName,
			"Cache Dir": D.CacheDir,
			"Src": src,
			"Dst": dst,
		}).Info("Accio - MINIO")
	reader, err := D.s3Client.GetObject(D.BktName, src)
	if err != nil {
		log.Errorf("MINIO: GetFile error %v", err)
		return err
	}
	
	outF, err := os.Create(dst)
	if err != nil {
		log.Errorf("MINIO: Cannot create dst file %v, err %v", dst, err)
		return err
	}
	defer outF.Close()

	_, err = io.Copy(outF, reader)
	if err != nil {
		os.Remove(dst)
	}
	return err
}
