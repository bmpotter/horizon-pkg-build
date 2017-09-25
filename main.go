package main

import (
	"fmt"
	"github.com/open-horizon/horizon-pkg-build/create"
	//	"github.com/open-horizon/rsapss-tool/sign"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/urfave/cli"
	"os"
)

const (
	version           = "0.1.0"
	outputInfoPrefix  = "[INFO]"
	outputDebugPrefix = "[DEBUG]"
	outputErrorPrefix = "[ERROR]"
)

type CheckType int

const (
	WRITEDIR CheckType = iota
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

func createAction(ctx *cli.Context) error {
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

	images := ctx.StringSlice("dockerimage")
	if len(images) == 0 {
		return cli.NewExitError("Required option(s) 'dockerimage' not provided. Use the '--help' option for more information", 2)
	}

	author := ctx.String("author")
	if author == "" {
		return cli.NewExitError("Required option 'author' not provided. Use the '--help' option for more information.", 2)
	}

	dockerEndpoint := ctx.String("dockerendpoint")
	if dockerEndpoint == "" {
		return cli.NewExitError("Required option 'dockerendpoint' not provided. Use the '--help' option for more information.", 2)
	}

	dockerClient, err := docker.NewClient(dockerEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Docker client setup error: %v\n", outputErrorPrefix, err)
		return cli.NewExitError("Docker client could not be set up.", 2)
	}

	err = dockerClient.Ping()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Endpoint connection error: %v\n", outputErrorPrefix, err)
		return cli.NewExitError(fmt.Sprintf("Docker endpoint %v Unreachable.", dockerEndpoint), 2)
	}

	parturlbase := ctx.String("parturlbase")
	if parturlbase == "" {
		return cli.NewExitError("Required option 'parturlbase' not provided. Use the '--help' option for more information.", 2)
	}

	// TODO: check that URL validates

	err = create.NewPkg(dockerClient, outputDir, author, privateKey, parturlbase, images)
	if err != nil {
		// TODO: improve output of possibly multiple errors
		fmt.Fprintf(os.Stderr, "%s Error(s) creating new Horizon Pkg: %v\n", outputErrorPrefix, err)
		// TODO: wrap this error in a cleanup operation
		return cli.NewExitError("Failed to create Horizon Pkg", 3)
	}

	return nil
}

func main() {
	app := cli.NewApp()
	app.EnableBashCompletion = true

	app.Name = "horizon-pkg-build"
	app.Version = version
	app.Usage = "Create, validate, and upload Horizon Pkg metadata and parts"

	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "debug", EnvVar: "HZNPKG_DEBUG"},
	}

	app.Action = func(ctx *cli.Context) error {
		if ctx.Bool("debug") {
			fmt.Fprintf(os.Stderr, "%s debug output enabled.\n", outputInfoPrefix)
		}
		return nil
	}

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
					Value:  "",
					Usage:  "A URL base (e.g. https://hovitos.engineering/hznpkg) that prefixes downloadable pkg parts output by this program. It is expected that the pkg directory written to the given outputdir (d) will be available at the given url base.",
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
			},
			Action: createAction,
		}}

	app.Run(os.Args)

	fmt.Fprintf(os.Stderr, "%s Exiting.\n", outputInfoPrefix)
	os.Exit(0)
}
