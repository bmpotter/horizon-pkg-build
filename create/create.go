package create

import (
	"compress/gzip"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/open-horizon/horizon-pkg-fetch/horizonpkg"
	"github.com/open-horizon/rsapss-tool/sign"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// Returns sha256hash, filename, full path to written file, and err.
// N.B. The hash is calculated on the *uncompressed* content; this is so we can change
// the compression mechanism (or different hosts can pick alt. storage formats) and we
// can still reliably check the signature after decompression.
func writeDockerImage(client *docker.Client, tmpDir string, image string) (string, hash.Hash, string, error) {
	dockerSafeName := strings.Replace(image, "/", "_", -1)

	tmpFile, err := ioutil.TempFile(tmpDir, dockerSafeName)
	if err != nil {
		return "", nil, "", err
	}
	defer tmpFile.Close()

	//fileBuffer := bufio.NewWriter(tmpFile)
	gzipFileWriter := gzip.NewWriter(tmpFile)
	// N.B. It's important that this match the signing tools' expectations, we reuse this hash
	hashWriter := sha256.New()
	multiWriter := io.MultiWriter(hashWriter, gzipFileWriter)

	opts := docker.ExportImageOptions{
		Name:              image,
		InactivityTimeout: time.Second * 20,
		OutputStream:      multiWriter,
	}

	if err := client.ExportImage(opts); err != nil {
		return "", nil, "", err
	}

	if err := gzipFileWriter.Flush(); err != nil {
		return "", nil, "", err
	}

	if err := gzipFileWriter.Close(); err != nil {
		return "", nil, "", err
	}

	if err := tmpFile.Sync(); err != nil {
		return "", nil, "", err
	}

	hash := fmt.Sprintf("%x", hashWriter.Sum(nil))

	fileName := fmt.Sprintf("%v.tar.gz", hash)
	permPath := path.Join(tmpDir, fileName)

	err = os.Rename(tmpFile.Name(), permPath)
	if err != nil {
		return "", nil, tmpFile.Name(), err
	}

	return fileName, hashWriter, permPath, err
}

// the worker part of the concurrent image processing operations
func exportDockerImage(group *sync.WaitGroup, client *docker.Client, tmpDir string, pkgBuilder *horizonpkg.PkgBuilder, image string, urlBase string, privateKey *rsa.PrivateKey) {
	fileName, hashWriter, filePath, err := writeDockerImage(client, tmpDir, image)
	if err != nil {
		fmt.Printf("ERR! %v", err)
		// TODO: need to plumb error writing business into this and / or write lock an errors array that can collect them
		// TODO: note that in case of err, filePath can be a file that needs cleanup
	}

	// TODO: upload the part here and verify with a HEAD request to it?
	// for now, just construct a URL for the part and write that in the pkg
	// upload the part with the appropriate path stuff (note: requires the pkg name so we can put it in the pkg subdir)

	// N.B. The signature is on the *uncompressed* content
	signature, err := sign.Sha256HashOfInput(privateKey, hashWriter)
	if err != nil {
		fmt.Printf("ERR! %v", err)
		// TODO: need to plumb error writing business into this and / or write lock an errors array that can collect them
		// TODO: note that in case of err, filePath can be a file that needs cleanup
	}

	signatures := []string{signature}

	// note: this assumes no funny business was done in writeDockerImage
	source := horizonpkg.PartSource{URL: fmt.Sprintf("%s/%s/%s", strings.TrimRight(urlBase, "/"), pkgBuilder.ID(), fileName)}

	// determine bytes of filePath
	info, err := os.Stat(filePath)
	if err != nil {
		fmt.Printf("ERR! %v", err)
		// TODO: need to plumb error writing business into this and / or write lock an errors array that can collect them
	}

	// we use the shasum as the name for the part
	sha256sum := fmt.Sprintf("%x", hashWriter.Sum(nil))
	_, err = pkgBuilder.AddPart(sha256sum, sha256sum, image, signatures, info.Size(), source)
	if err != nil {
		fmt.Printf("ERR! %v", err)
		// TODO: need to plumb error writing business into this and / or write lock an errors array that can collect them
	}

	group.Done()
}

func NewPkg(client *docker.Client, baseOutputDir string, author string, privateKey string, urlBase string, images []string) error {

	pK, err := sign.ReadPrivateKey(privateKey)
	if err != nil {
		return err
	}

	pkgBuilder, err := horizonpkg.NewDockerImagePkgBuilder(horizonpkg.FILE, author, images)
	if err != nil {
		return err
	}

	// we leave around the tmpDir if we fail so it can be inspected
	tmpDir, err := ioutil.TempDir(baseOutputDir, fmt.Sprintf("build-hznpkg-%s-", pkgBuilder.ID()))
	if err != nil {
		return err
	}

	var waitGroup sync.WaitGroup

	// concurrently process each part
	for _, image := range images {
		waitGroup.Add(1)
		go exportDockerImage(&waitGroup, client, tmpDir, pkgBuilder, image, urlBase, pK)
	}

	waitGroup.Wait()

	_, serialized, err := pkgBuilder.Build()
	if err != nil {
		return err
	}

	pkgFile := path.Join(baseOutputDir, fmt.Sprintf("%s.json", pkgBuilder.ID()))
	err = ioutil.WriteFile(pkgFile, serialized, 0644)
	if err != nil {
		return err
	}

	// and sign the pkg file content
	pkgSig, err := sign.Input(privateKey, serialized)
	if err != nil {
		return err
	}

	pkgSigFile := fmt.Sprintf("%s.sig", pkgFile)
	err = ioutil.WriteFile(pkgSigFile, []byte(pkgSig), 0644)

	// all succeeded, move tmp dir
	err = os.Rename(tmpDir, path.Join(baseOutputDir, string(os.PathSeparator), pkgBuilder.ID()))
	if err != nil {
		return err
	}

	return nil
}
