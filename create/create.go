package create

import (
	"compress/gzip"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/open-horizon/horizon-pkg-build/cmdtools"
	"github.com/open-horizon/horizon-pkg-fetch/horizonpkg"
	"github.com/open-horizon/rsapss-tool/sign"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"
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

	gzipFileWriter := gzip.NewWriter(tmpFile)
	// N.B. It's important that this match the signing tools' expectations, we reuse this hash
	hashWriter := sha256.New()
	multiWriter := io.MultiWriter(hashWriter, gzipFileWriter)

	opts := docker.ExportImageOptions{
		Name:         image,
		OutputStream: multiWriter,
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
func exportDockerImage(reporter *cmdtools.SynchronizedReporter, group *sync.WaitGroup, client *docker.Client, tmpDir string, pkgBuilder *horizonpkg.PkgBuilder, image string, urlBase string, privateKey *rsa.PrivateKey) {
	defer group.Done()

	fmt.Fprintf(reporter.ErrWriter, "%s Beginning processing Docker image: %v\n", cmdtools.OutputInfoPrefix, image)

	fileName, hashWriter, filePath, err := writeDockerImage(client, tmpDir, image)
	if err != nil {
		// TODO: differentiate b/n errors here: user can specify an image that isn't in the local repo and the client will fail
		reporter.DelegateErr(false, true, fmt.Sprintf("Error writing docker image %v. Error: %v", image, err))
		return
	}

	fmt.Fprintf(reporter.ErrWriter, "%s Wrote Docker image %v as: %v\n", cmdtools.OutputInfoPrefix, image, fileName)

	// TODO: upload the part here and verify with a HEAD request to it?
	// for now, just construct a URL for the part and write that in the pkg
	// upload the part with the appropriate path stuff (note: requires the pkg name so we can put it in the pkg subdir)

	// N.B. The signature is on the *uncompressed* content
	signature, err := sign.Sha256HashOfInput(privateKey, hashWriter)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error hashing docker image %v. Error: %v", image, err))
		return
	}

	fmt.Fprintf(reporter.ErrWriter, "%s Signed hash for image: %v\n", cmdtools.OutputInfoPrefix, image)

	signatures := []string{signature}

	// note: this assumes no funny business was done in writeDockerImage
	source := horizonpkg.PartSource{URL: fmt.Sprintf("%s/%s/%s", strings.TrimRight(urlBase, "/"), pkgBuilder.ID(), fileName)}

	// determine bytes of filePath
	info, err := os.Stat(filePath)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error processing docker image %v. Error: %v", image, err))
		return
	}

	// we use the shasum as the name for the part
	sha256sum := fmt.Sprintf("%x", hashWriter.Sum(nil))
	_, err = pkgBuilder.AddPart(sha256sum, sha256sum, image, signatures, info.Size(), source)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error adding Pkg part %v. Error: %v", sha256sum, err))
		return
	}

	fmt.Fprintf(reporter.ErrWriter, "%s Part added to pkg %v for image: %v\n", cmdtools.OutputInfoPrefix, pkgBuilder.ID(), image)
}

// NewPkg is an exported function that fulfills the primary use case of this
// module: create a new package and output all relevant material for upload /
// service to a Horizon edge node.
func NewPkg(reporter *cmdtools.SynchronizedReporter, client *docker.Client, baseOutputDir string, author string, privateKey string, urlBase string, images []string) {

	pK, err := sign.ReadPrivateKey(privateKey)
	if err != nil {
		reporter.DelegateErr(true, true, fmt.Sprintf("Error reading RSA PSS private key. Error: %v\n", err))
		return
	}

	pkgBuilder, err := horizonpkg.NewDockerImagePkgBuilder(horizonpkg.FILE, author, images)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error setting up Pkg builder. Error: %v\n", err))
		return
	}

	// we leave around the tmpDir if we fail so it can be inspected
	tmpDir, err := ioutil.TempDir(baseOutputDir, fmt.Sprintf("build-hznpkg-%s-", pkgBuilder.ID()))
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error setting up Pkg builder. Error: %v\n", err))
		return
	}
	defer os.RemoveAll(tmpDir)

	fmt.Fprintf(reporter.ErrWriter, "%s Created temporary directory for packaging: %v\n", cmdtools.OutputInfoPrefix, tmpDir)

	var waitGroup sync.WaitGroup

	// concurrently process each part
	for _, image := range images {
		waitGroup.Add(1)
		go exportDockerImage(reporter, &waitGroup, client, tmpDir, pkgBuilder, image, urlBase, pK)
	}

	waitGroup.Wait()
	if reporter.DelegateErrorCount > 0 {
		// error reporting is done elsewhere, we just need to manage the control flow
		fmt.Fprintf(reporter.ErrWriter, "%s All parts not processed successfully, discontinuing operations\n", cmdtools.OutputErrorPrefix)
		return
	}

	_, serialized, err := pkgBuilder.Build()
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error building package. Error: %v\n", err))
		return
	}

	pkgFile := path.Join(baseOutputDir, fmt.Sprintf("%s.json", pkgBuilder.ID()))
	err = ioutil.WriteFile(pkgFile, serialized, 0644)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error writing Pkg metadata to disk. Error: %v\n", err))
		return
	}
	fmt.Fprintf(reporter.ErrWriter, "%s Wrote pkg metadata file to: %v\n", cmdtools.OutputInfoPrefix, pkgFile)

	// and sign the pkg file content
	pkgSig, err := sign.Input(privateKey, serialized)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error signing Pkg metadata. Error: %v\n", err))
		return
	}

	pkgSigFile := fmt.Sprintf("%s.sig", pkgFile)
	err = ioutil.WriteFile(pkgSigFile, []byte(pkgSig), 0644)

	fmt.Fprintf(reporter.ErrWriter, "%s Signed pkg metadata file and wrote signature to file: %v\n", cmdtools.OutputInfoPrefix, pkgSigFile)

	// all succeeded, move tmp dir
	permDir := path.Join(baseOutputDir, string(os.PathSeparator), pkgBuilder.ID())
	err = os.Rename(tmpDir, permDir)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error moving Pkg content to permanent dir from tmpdir. Error: %v\n", err))
		return
	}

	fmt.Fprintf(reporter.ErrWriter, "%s Pkg content preparation finished. Temporary files removed and pkg content written to %v\n", cmdtools.OutputInfoPrefix, permDir)
	fmt.Fprintf(reporter.OutWriter, "%v %v %v\n", permDir, pkgFile, pkgSigFile)
}
