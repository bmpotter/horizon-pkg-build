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
	"path/filepath"
	"strings"
	"sync"
)

func imageExistsAtTarget(client *docker.Client, image string) (bool, error) {
	opts := docker.ListImagesOptions{
		All:    true,
		Filter: image,
	}

	images, err := client.ListImages(opts)
	if err != nil {
		return false, err
	}

	for _, im := range images {
		for _, t := range im.RepoTags {
			if t == image {
				return true, nil
			}
		}
	}
	return false, nil

}

func exportImageToFile(client *docker.Client, authConfigurations *docker.AuthConfigurations, tmpDir string, image string) (string, string, error) {

	dockerSafeName := strings.Replace(image, "/", "_", -1)

	dockerSafeTmpFileName := fmt.Sprintf("%s.tar", dockerSafeName)
	tmpFile, err := ioutil.TempFile(tmpDir, dockerSafeTmpFileName)
	if err != nil {
		return "", "", err
	}
	defer tmpFile.Close()

	// fetch image if it doesn't exist locally
	imageExists, err := imageExistsAtTarget(client, image)
	if err != nil {
		return "", "", err
	}

	if !imageExists {
		spl := strings.Split(image, ":")

		if len(spl) != 2 {
			return "", "", fmt.Errorf("Unable to parse given image name: %v", image)
		}

		repo := spl[0]
		var repoAuth docker.AuthConfiguration

		if authConfigurations != nil {
			serverAddressSpl := strings.Split(repo, "/")
			var serverAddress string

			if len(serverAddressSpl) > 1 {
				serverAddress = serverAddressSpl[0]

				for _, ra := range authConfigurations.Configs {
					if ra.ServerAddress == serverAddress {
						repoAuth = ra
					}
				}
			} // if we didn't find one, we'll try the pull without
		}

		pullOpts := docker.PullImageOptions{
			Repository: repo,
			Tag:        spl[1],
		}

		// TODO: support authenticated pull
		if err := client.PullImage(pullOpts, repoAuth); err != nil {
			return "", "", err
		}
	}

	// pulled by now
	exportOpts := docker.ExportImageOptions{
		Name:         image,
		OutputStream: tmpFile,
	}

	if err := client.ExportImage(exportOpts); err != nil {
		return "", "", err
	}

	if err := tmpFile.Sync(); err != nil {
		return "", "", err
	}

	return tmpFile.Name(), dockerSafeTmpFileName, nil
}

func compressImageFile(tmpDir string, fileName string, dockerSafeTmpFileName string) (string, string, int64, error) {

	dockerSafeTmpCompressedFileName := fmt.Sprintf("%s.tgz", dockerSafeTmpFileName[0:len(dockerSafeTmpFileName)-len(filepath.Ext(dockerSafeTmpFileName))])
	tmpCompressedFile, err := ioutil.TempFile(tmpDir, dockerSafeTmpCompressedFileName)
	if err != nil {
		return "", "", 0, err
	}
	defer tmpCompressedFile.Close()

	// now compress
	gzipFileWriter, err := gzip.NewWriterLevel(tmpCompressedFile, gzip.BestCompression)
	if err != nil {
		return "", "", 0, err
	}
	defer gzipFileWriter.Close()

	tmpFile, err := os.Open(fileName)
	if err != nil {
		return "", "", 0, err
	}
	defer tmpFile.Close()

	unzippedBytes, err := io.Copy(gzipFileWriter, tmpFile)
	if err != nil {
		return "", "", 0, err
	}

	if err := gzipFileWriter.Flush(); err != nil {
		return "", "", 0, err
	}

	return tmpCompressedFile.Name(), dockerSafeTmpCompressedFileName, unzippedBytes, nil
}

// Returns sha256hash, filename, full path to written file, and err.
// N.B. The hash is calculated on the *compressed* content.
func writeDockerImage(client *docker.Client, authConfigurations *docker.AuthConfigurations, tmpDir string, image string) (hash.Hash, string, string, int64, error) {

	tmpFileName, dockerSafeTmpFileName, err := exportImageToFile(client, authConfigurations, tmpDir, image)
	if err != nil {
		return nil, "", "", 0, err
	}
	defer os.Remove(tmpFileName)

	tmpCompressedFileName, dockerSafeTmpCompressedFileName, _, err := compressImageFile(tmpDir, tmpFileName, dockerSafeTmpFileName)
	if err != nil {
		return nil, "", "", 0, err
	}

	tmpCompressedFile, err := os.Open(tmpCompressedFileName)
	if err != nil {
		return nil, "", "", 0, err
	}

	// N.B. It's important that this match the signing tools' expectations, we reuse this hash
	hashWriter := sha256.New()
	compressedBytes, err := io.Copy(hashWriter, tmpCompressedFile)
	if err != nil {
		return nil, "", "", 0, err
	}

	tmpCompressedFile.Close()

	hash := fmt.Sprintf("%x", hashWriter.Sum(nil))

	fileName := fmt.Sprintf("%v%s", hash, filepath.Ext(dockerSafeTmpCompressedFileName))
	permPath := path.Join(tmpDir, fileName)

	if err := os.Chmod(tmpCompressedFile.Name(), 0644); err != nil {
		return nil, "", tmpCompressedFile.Name(), 0, err
	}

	if err := os.Rename(tmpCompressedFile.Name(), permPath); err != nil {
		return nil, "", tmpCompressedFile.Name(), 0, err
	}

	// N.B. The temporary files get removed when the tmpdir containing them does in the event of an error

	return hashWriter, fileName, permPath, compressedBytes, err
}

// the worker part of the concurrent image processing operations
func exportDockerImage(reporter *cmdtools.SynchronizedReporter, group *sync.WaitGroup, client *docker.Client, authConfigurations *docker.AuthConfigurations, tmpDir string, pkgBuilder *horizonpkg.PkgBuilder, image string, urlBase string, privateKey *rsa.PrivateKey) {
	defer group.Done()

	fmt.Fprintf(reporter.ErrWriter, "%s Beginning processing Docker image: %v\n", cmdtools.OutputInfoPrefix, image)

	hashWriter, fileName, _, compressedBytes, err := writeDockerImage(client, authConfigurations, tmpDir, image)
	if err != nil {
		// TODO: differentiate b/n errors here: user can specify an image that isn't in the local repo and the client will fail
		reporter.DelegateErr(false, true, fmt.Sprintf("Error writing docker image %v. Error: %v\n", image, err))
		return
	}

	fmt.Fprintf(reporter.ErrWriter, "%s Wrote Docker image %v as: %v\n", cmdtools.OutputInfoPrefix, image, fileName)

	// TODO: upload the part here and verify with a HEAD request to it?
	// for now, just construct a URL for the part and write that in the pkg
	// upload the part with the appropriate path stuff (note: requires the pkg name so we can put it in the pkg subdir)

	// N.B. The signature is on the *uncompressed* content
	signature, err := sign.Sha256HashOfInput(privateKey, hashWriter)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error hashing docker image %v. Error: %v\n", image, err))
		return
	}

	fmt.Fprintf(reporter.ErrWriter, "%s Signed hash for image: %v\n", cmdtools.OutputInfoPrefix, image)

	signatures := []string{signature}

	// note: this assumes no funny business was done in writeDockerImage
	source := horizonpkg.PartSource{URL: fmt.Sprintf("%s/%s/%s", strings.TrimRight(urlBase, "/"), pkgBuilder.ID(), fileName)}

	// we use the shasum as the name for the part
	sha256sum := fmt.Sprintf("%x", hashWriter.Sum(nil))
	_, err = pkgBuilder.AddPart(sha256sum, sha256sum, image, signatures, compressedBytes, source)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error adding Pkg part %v. Error: %v\n", sha256sum, err))
		return
	}

	fmt.Fprintf(reporter.ErrWriter, "%s Part added to pkg %v for image: %v\n", cmdtools.OutputInfoPrefix, pkgBuilder.ID(), image)
}

// NewPkg is an exported function that fulfills the primary use case of this
// module: create a new package and output all relevant material for upload /
// service to a Horizon edge node.
func NewPkg(reporter *cmdtools.SynchronizedReporter, client *docker.Client, authConfigurations *docker.AuthConfigurations, baseOutputDir string, author string, privateKey string, urlBase string, images []string) (string, string, string) {

	pK, err := sign.ReadPrivateKey(privateKey)
	if err != nil {
		reporter.DelegateErr(true, true, fmt.Sprintf("Error reading RSA PSS private key. Error: %v\n", err))
		return "", "", ""
	}

	pkgBuilder, err := horizonpkg.NewDockerImagePkgBuilder(horizonpkg.FILE, author, images)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error setting up Pkg builder. Error: %v\n", err))
		return "", "", ""
	}

	tmpDir, err := ioutil.TempDir(baseOutputDir, fmt.Sprintf("build-hznpkg-%s-", pkgBuilder.ID()))
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error setting up Pkg builder. Error: %v\n", err))
		return "", "", ""
	}
	defer os.RemoveAll(tmpDir)

	fmt.Fprintf(reporter.ErrWriter, "%s Created temporary directory for packaging: %v\n", cmdtools.OutputInfoPrefix, tmpDir)

	var waitGroup sync.WaitGroup

	// concurrently process each part
	for _, image := range images {
		waitGroup.Add(1)
		go func(image string) {
			exportDockerImage(reporter, &waitGroup, client, authConfigurations, tmpDir, pkgBuilder, image, urlBase, pK)
		}(image)
	}

	waitGroup.Wait()
	if reporter.DelegateErrorCount > 0 {
		// error reporting is done elsewhere, we just need to manage the control flow
		fmt.Fprintf(reporter.ErrWriter, "%s All parts not processed successfully, discontinuing operations\n", cmdtools.OutputErrorPrefix)
		return "", "", ""
	}

	_, serialized, err := pkgBuilder.Build()
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error building package. Error: %v\n", err))
		return "", "", ""
	}

	pkgFile := path.Join(baseOutputDir, fmt.Sprintf("%s.json", pkgBuilder.ID()))
	err = ioutil.WriteFile(pkgFile, serialized, 0644)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error writing Pkg metadata to disk. Error: %v\n", err))
		return "", "", ""
	}
	fmt.Fprintf(reporter.ErrWriter, "%s Wrote pkg metadata file to: %v\n", cmdtools.OutputInfoPrefix, pkgFile)

	// and sign the pkg file content
	pkgSig, err := sign.Input(privateKey, serialized)
	if err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error signing Pkg metadata. Error: %v\n", err))
		return "", "", ""
	}

	pkgSigFile := fmt.Sprintf("%s.sig", pkgFile)
	err = ioutil.WriteFile(pkgSigFile, []byte(pkgSig), 0644)

	fmt.Fprintf(reporter.ErrWriter, "%s Signed pkg metadata file and wrote signature to file: %v\n", cmdtools.OutputInfoPrefix, pkgSigFile)

	// all succeeded, change perms then move tmp dir
	if err := os.Chmod(tmpDir, 0755); err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error changing perms on tmpdir. Error: %v\n", err))
		return "", "", ""
	}

	permDir := path.Join(baseOutputDir, string(os.PathSeparator), pkgBuilder.ID())
	if err := os.Rename(tmpDir, permDir); err != nil {
		reporter.DelegateErr(false, true, fmt.Sprintf("Error moving Pkg content to permanent dir from tmpdir. Error: %v\n", err))
		return "", "", ""
	}

	// success
	return permDir, pkgFile, pkgSigFile
}
