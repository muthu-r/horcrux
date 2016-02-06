//
// Implements local host cp access
// - mostly for testing and verifying before uploading to remote location
//
package cp

import (
	"io"
	"os"

	log "github.com/Sirupsen/logrus"
)

type Data struct {
	SrcDir   string
	CacheDir string
}

func (D Data) String() string {
	return "CP:: " + " Source Dir: " + D.SrcDir + " CacheDir: " + D.CacheDir
}

func (D Data) Name() string {
	return "CP"
}

func (D Data) Init() (string, error) {
	log.WithFields(log.Fields{
		"Src Dir":   D.SrcDir,
		"Cache Dir": D.CacheDir,
	}).Info("Accio - CP")
	return D.SrcDir, nil
}

func (D Data) GetFile(src string, dst string) error {
	log.WithFields(
		log.Fields{
			"SRC": src,
			"DST": dst,
		}).Debug("Accio: CP - GetFile")

	inF, err := os.Open(src)
	if err != nil {
		log.Errorf("Accio: cp: Cannot open src file %v, err %v", src, err)
		return err
	}
	defer inF.Close()

	outF, err := os.Create(dst)
	if err != nil {
		log.Errorf("Accio: cp: Cannot create dst file %v, err %v", dst, err)
		return err
	}
	defer outF.Close()

	_, err = io.Copy(outF, inF)
	return err
}
