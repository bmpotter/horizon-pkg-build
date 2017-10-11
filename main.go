package main

import (
	"fmt"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/open-horizon/horizon-pkg-build/cmdtools"
	"github.com/open-horizon/horizon-pkg-build/create"
	"github.com/urfave/cli"
	"net/url"
	"os"
	"time"
)

// CheckType is a quasi-enum for FS object checking
type CheckType int

const (
	// WRITEDIR describes a writable fs directory
	WRITEDIR CheckType = iota

	// EXISTINGFILE describes a readable file type
	EXISTINGFILE
)

func checkAccess(ty CheckType, target string) error {
	outputCheck, err := os.Stat(target)
	if err != nil {
		return cli.NewExitError(fmt.Sprintf("Unable to stat %v", target), 2)
	}

	mode := outputCheck.Mode()

	switch ty {
	case WRITEDIR:
		if !mode.IsDir() {
			return cli.NewExitError(fmt.Sprintf("Directory path (%v) is unusable", target), 2)
		}
	case EXISTINGFILE:
		if !mode.IsRegular() {
			return cli.NewExitError(fmt.Sprintf("File (%v) is unusable", target), 2)
		}
	default:
		return fmt.Errorf("Unknown CheckType: %T", ty)
	}

	return nil
}

func dockerConnect(ctx *cli.Context) (*docker.Client, error) {
	dockerEndpoint := ctx.String("dockerendpoint")
	if dockerEndpoint == "" {
		return nil, cli.NewExitError("Required option 'dockerendpoint' not provided. Use the '--help' option for more information.", 2)
	}

	dockerClient, err := docker.NewClient(dockerEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Docker client setup error: %v\n", cmdtools.OutputErrorPrefix, err)
		return nil, cli.NewExitError("Docker client could not be set up.", 2)
	}

	err = dockerClient.Ping()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Endpoint connection error: %v\n", cmdtools.OutputErrorPrefix, err)
		return nil, cli.NewExitError(fmt.Sprintf("Docker endpoint %v Unreachable.", dockerEndpoint), 2)
	}

	return dockerClient, nil
}

func createAction(reporter *cmdtools.SynchronizedReporter, ctx *cli.Context) error {
	outputDir := ctx.String("outputdir")
	if outputDir == "" {
		return cli.NewExitError("Required option 'outputDir' not provided. Use the '--help' option for more information.", 2)
	}

	if err := checkAccess(WRITEDIR, outputDir); err != nil {
		return cli.NewExitError(fmt.Sprintf("Error using given output directory: %v", err), 2)
	}

	privateKey := ctx.String("privatekey")
	if privateKey == "" {
		return cli.NewExitError("Required option 'privatekey' not provided. Use the '--help' option for more information.", 2)
	}

	if err := checkAccess(EXISTINGFILE, privateKey); err != nil {
		return cli.NewExitError(fmt.Sprintf("Error accessing privateKey: %v", err), 2)
	}

	dockerClient, err := dockerConnect(ctx)
	if err != nil {
		return err // already a cli error
	}

	images := ctx.StringSlice("dockerimage")
	if len(images) == 0 {
		return cli.NewExitError("Required option(s) 'dockerimage' not provided. Use the '--help' option for more information", 2)
	}

	author := ctx.String("author")
	if author == "" {
		return cli.NewExitError("Required option 'author' not provided. Use the '--help' option for more information.", 2)
	}

	parturlbase := ctx.String("parturlbase")
	if parturlbase == "" {
		return cli.NewExitError("Required option 'parturlbase' not provided. Use the '--help' option for more information.", 2)
	} else if _, err := url.Parse(parturlbase); err != nil {
		return cli.NewExitError(fmt.Sprintf("Unable to use provided value for 'parturlbase'. Error: %v", err), 2)
	}

	var authConfigurations *docker.AuthConfigurations
	readauthconfig := ctx.Bool("readauthconfig")
	if !readauthconfig {
		fmt.Fprintf(os.Stderr, "%s Option 'readauthconfig' not set, proceeding without credentialed requests.\n", cmdtools.OutputInfoPrefix)
	} else {
		var err error
		authConfigurations, err = docker.NewAuthConfigurationsFromDockerCfg()
		if err != nil {
			return cli.NewExitError(fmt.Sprintf("Unable to read authentication information from Docker configuration files. Set DOCKER_CONFIG envvar to a configuration file path or put a proper Docker configuration file in one its common locations. Error: %v", err), 2)
		}
	}

	skippull := ctx.Bool("skippull")
	if skippull {
		fmt.Fprintf(os.Stderr, "%s Option 'skippull' set, this tool will now skip performing a Docker pull from target registry", cmdtools.OutputInfoPrefix)
	}

	var delegateError error
	reporter.DelegateErrorConsumer(func(e cmdtools.DelegateError) {
		fmt.Fprintf(os.Stderr, "%s Error creating new Pkg: %v", cmdtools.OutputErrorPrefix, e.Error())

		var code int
		if e.UserError {
			code = 2
		} else {
			code = 3
		}

		delegateError = cli.NewExitError("Failed to create Pkg", code)
	})

	// do the work; any breaking errors will cause DelegateErrorConsumer call its function handler
	permDir, pkgFile, pkgSigFile := create.NewPkg(reporter, dockerClient, skippull, authConfigurations, outputDir, author, privateKey, parturlbase, images)
	if delegateError == nil {
		fmt.Fprintf(reporter.ErrWriter, "%s Pkg content preparation finished. Temporary files removed and pkg content written to %v\n", cmdtools.OutputInfoPrefix, permDir)
		fmt.Fprintf(reporter.OutWriter, "%v %v %v\n", permDir, pkgFile, pkgSigFile)
	}
	return delegateError
}

func main() {
	app := cli.NewApp()
	app.EnableBashCompletion = true

	app.Name = "horizon-pkg-build"
	app.Version = cmdtools.Version
	app.Usage = "Create, validate, and upload Horizon Pkg metadata and parts"

	// TODO: support debug with more logging
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "debug", EnvVar: "HZNPKG_DEBUG"},
	}

	app.Action = func(ctx *cli.Context) error {
		if ctx.Bool("debug") {
			fmt.Fprintf(os.Stderr, "%s debug output enabled.\n", cmdtools.OutputInfoPrefix)
		}
		return nil
	}

	// set up reporter
	reporter := cmdtools.NewSynchronizedReporter(512, time.Duration(5*time.Millisecond))

	app.Commands = []cli.Command{
		cli.Command{
			Name:    "create",
			Aliases: []string{"c"},
			Usage:   "Create a new Horizon Pkg from Docker image files",
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:  "dockerimage, i",
					Usage: "Docker image name and tag to package (i.e. 'summit.hovitos.engineering/x86/gt-db:0.1.0'). May be specified multiple times",
				},
				cli.StringFlag{
					Name:   "outputdir, d",
					Value:  ".",
					Usage:  "Path to which Horizon Pkg output files will be written",
					EnvVar: "HZNPKG_OUTPUTDIR",
				},
				cli.StringFlag{
					Name:   "parturlbase, u",
					Value:  "/",
					Usage:  "A URL base (e.g. https://hovitos.engineering/hznpkg) that prefixes downloadable pkg parts output by this program. It is expected that the pkg directory written to the given outputdir (d) will be available at the given url base. Note that '/' is valid and indicates that the Pkg parts will be served from the same domain as the output Pkg metadata file",
					EnvVar: "HZNPKG_URLBASE",
				},
				cli.StringFlag{
					Name:   "privatekey, k",
					Value:  "",
					Usage:  "PEM-encoded private key to sign the payload",
					EnvVar: "RSAPSSTOOL_PRIVATEKEY",
				},
				cli.StringFlag{
					Name:   "author, a",
					Value:  "",
					Usage:  "Email address of the author of this Horizon pkg",
					EnvVar: "HZNPKG_AUTHOR",
				},
				cli.StringFlag{
					Name:   "dockerendpoint, de",
					Value:  "unix:///var/run/docker.sock",
					Usage:  "Local or remote Docker API endpoint from which images will be fetched",
					EnvVar: "HZNPKG_DOCKERENDPOINT",
				},
				cli.BoolFlag{
					Name:   "readauthconfig, ra",
					Usage:  "Enable reading authentication information from a Docker configuration file, e.g. $HOME/.docker/config.json, $HOME/.dockercfg, or path pointed-to by envvar DOCKER_CONFIG",
					EnvVar: "HZNPKG_READAUTHCONFIG",
				},
				cli.BoolFlag{
					Name:   "skippull, sp",
					Usage:  "Skip performing a Docker pull if a requested Docker image exists in the registry already",
					EnvVar: "HZNPKG_SKIPPULL",
				},
			},
			// curry the action with an anonymous function so we can get a reporter passed
			Action: func(ctx *cli.Context) error { return createAction(reporter, ctx) },
		},
	}

	app.Run(os.Args)

	fmt.Fprintf(os.Stderr, "%s Exiting.\n", cmdtools.OutputInfoPrefix)
	os.Exit(0)
}

// a BoolFlag is false by default, BoolT is true by default
