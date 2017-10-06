// +build integration

package create

import (
	docker "github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"io"
	"io/ioutil"
	"testing"
)

const bogusImageContent = "fffff"

type MockDockerClient struct {
	mock.Mock
}

func (c *MockDockerClient) ExportImage(opts docker.ExportImageOptions) error {
	args := c.Called(opts)

	// actually write something if opts is not empty
	if opts.Name == "foo.goo/someimage:0.2.0" {
		_, err := io.WriteString(opts.OutputStream, bogusImageContent)
		if err != nil {
			return err
		}
	}

	return args.Error(0)
}

func (c *MockDockerClient) ListImages(opts docker.ListImagesOptions) ([]docker.APIImages, error) {
	args := c.Called(opts)
	return args.Get(0).([]docker.APIImages), args.Error(1)
}

func (c *MockDockerClient) PullImage(opts docker.PullImageOptions, auth docker.AuthConfiguration) error {
	args := c.Called(opts, auth)
	return args.Error(0)
}

func setup() (string, error) {
	dir, err := ioutil.TempDir("", "create-newPkg-")
	if err != nil {
		return "", err
	}

	return dir, err
}

func Test_Create_NewPkg_Suite(suite *testing.T) {
	tmpDir, err := setup()
	assert.Nil(suite, err)

	suite.Run("exportImageToFile pulls w/ unauthenticated request if no local image found", func(t *testing.T) {
		listOpts := docker.ListImagesOptions{All: true, Filter: "domain.com/someimage:0.1.0"}

		m := new(MockDockerClient)
		// check that it searches for the right container image
		m.On("ListImages", listOpts).Return([]docker.APIImages{}, nil)

		// we don't care what gets passed to most of these, just that the auth config is empty
		m.On("PullImage", mock.AnythingOfType("docker.PullImageOptions"), docker.AuthConfiguration{}).Return(nil)
		m.On("ExportImage", mock.AnythingOfType("docker.ExportImageOptions")).Return(nil)

		// these creds don't match
		_, _, err := exportImageToFile(m, true, &docker.AuthConfigurations{Configs: map[string]docker.AuthConfiguration{"someid": docker.AuthConfiguration{Username: "foo", ServerAddress: "somenonmatchingdomain.com"}}}, tmpDir, "domain.com/someimage:0.1.0")
		assert.Nil(t, err)

		m.AssertExpectations(t)
	})

	suite.Run("exportImageToFile pulls w/ authenticated request if local image not found and creds provided", func(t *testing.T) {
		m := new(MockDockerClient)
		m.On("ListImages", mock.AnythingOfType("docker.ListImagesOptions")).Return([]docker.APIImages{}, nil)

		// we don't care what gets passed to most of these, just that the auth config matches
		m.On("PullImage", mock.AnythingOfType("docker.PullImageOptions"), docker.AuthConfiguration{Username: "timmy", ServerAddress: "xy.io"}).Return(nil)
		m.On("ExportImage", mock.AnythingOfType("docker.ExportImageOptions")).Return(nil)

		// these creds don't match
		_, _, err := exportImageToFile(m, true, &docker.AuthConfigurations{Configs: map[string]docker.AuthConfiguration{"someid": docker.AuthConfiguration{Username: "timmy", ServerAddress: "xy.io"}}}, tmpDir, "xy.io/someimage:0.1.0")
		assert.Nil(t, err)

		m.AssertExpectations(t)
	})

	suite.Run("exportImageToFile skips pull if image exists and we use default skip arg", func(t *testing.T) {
		m := new(MockDockerClient)
		m.On("ListImages", mock.AnythingOfType("docker.ListImagesOptions")).Return([]docker.APIImages{docker.APIImages{RepoTags: []string{"xy.io/someimage:0.1.0"}}}, nil)
		m.On("ExportImage", mock.AnythingOfType("docker.ExportImageOptions")).Return(nil)

		_, _, err := exportImageToFile(m, true, &docker.AuthConfigurations{}, tmpDir, "xy.io/someimage:0.1.0")
		assert.Nil(t, err)

		// want to make sure the pull didn't occur
		m.AssertNotCalled(t, "docker.PullImage", mock.AnythingOfType("docker.PullImageOptions"), mock.AnythingOfType("docker.AuthConfiguration"))
		m.AssertExpectations(t)
	})

	suite.Run("exportImageToFile *does not* skip pull if image exists and we set skip arg to false", func(t *testing.T) {
		m := new(MockDockerClient)
		m.On("ListImages", mock.AnythingOfType("docker.ListImagesOptions")).Return([]docker.APIImages{docker.APIImages{RepoTags: []string{"xy.io/someimage:0.1.0"}}}, nil)
		m.On("PullImage", mock.AnythingOfType("docker.PullImageOptions"), mock.AnythingOfType("docker.AuthConfiguration")).Return(nil)
		m.On("ExportImage", mock.AnythingOfType("docker.ExportImageOptions")).Return(nil)

		// the "false" is important here
		_, _, err := exportImageToFile(m, false, &docker.AuthConfigurations{}, tmpDir, "xy.io/someimage:0.1.0")
		assert.Nil(t, err)

		m.AssertExpectations(t)
	})

	suite.Run("exportImageToFile", func(t *testing.T) {
		imageList := []docker.APIImages{docker.APIImages{ID: "1", RepoTags: []string{"foo.goo/someimage:0.2.0"}}}

		m := new(MockDockerClient)
		m.On("ListImages", mock.AnythingOfType("docker.ListImagesOptions")).Return(imageList, nil)
		// unfortunately, we can't check the options b/c of the changing file handle
		m.On("ExportImage", mock.AnythingOfType("docker.ExportImageOptions")).Return(nil)

		fName, _, err := exportImageToFile(m, true, &docker.AuthConfigurations{}, tmpDir, imageList[0].RepoTags[0])
		assert.Nil(t, err)
		assert.NotNil(t, fName)

		b, err := ioutil.ReadFile(fName)
		assert.Nil(t, err)

		assert.Equal(t, bogusImageContent, string(b))

		m.AssertExpectations(t)
	})
}
