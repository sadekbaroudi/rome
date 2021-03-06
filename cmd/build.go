// Copyright © 2017 Jon Whitcraft
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"sync"
	"strings"
	"os"
	"path"
	"time"
	"path/filepath"
	"github.com/jwhitcraft/rome/utils"
	"github.com/jwhitcraft/rome/build"
)

var (
	flavor string
	version string
	destination string
	source string

	clean bool = false

	fileWorkers int = 40
	fileBufferSize int = 4096

	linkWorkers int = 5
	linkBufferSize int = 2048
)

type File string
type Link struct {
	Link string
	Target string
}

// buildCmd represents the build command
var buildCmd = &cobra.Command{
	Use:   "build [FLAGS] SOURCE-FOLDER",
	Short: "Build SugarCRM",
	ValidArgs: []string{"source"},
	Long: `This will take a source version of Sugar and substitute out all the necessary build tags and create an
	installable copy of Sugar for you to use and dev on.`,
	PreRun: func(cmd *cobra.Command, args[]string) {
		// in the preRun, make sure that the source and destination exists
		source = args[0]

		destExists, err := exists(destination)
		if err != nil || !destExists {
			fmt.Printf("Destination Path (%s) does not exists, Creating Now\n", destination)
			os.MkdirAll(destination, 0775)
			// since we had to create the destination dir, set clean to false
			clean = false
		}

		sourceExists, err := exists(source)
		if err != nil || !sourceExists {
			fmt.Printf("\n\nSource Path (%s) does not exists!!\n\n", source)
			os.Exit(401)
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		if clean {
			fmt.Println("Cleaning " + destination)
			err := build.CleanBuild(destination)
			if err != nil {
				fmt.Println("Could Not Clean: " + destination)
				os.Exit(1)
			}
		}
		source = args[0]
		fmt.Println("Starting Rome on " + source + "...")
		defer utils.TimeTrack(time.Now())
		var builtFiles utils.Counter
		files := make(chan File, fileBufferSize)
		links := make(chan Link, linkBufferSize)
		quit := make(chan bool)
		var wg sync.WaitGroup
		var linkWg sync.WaitGroup

		// spawn 5 workers
		for i := 0; i < fileWorkers; i++ {
			wg.Add(1)
			go fileWorker(files, quit, &wg)
		}

		for i := 0; i < linkWorkers; i++ {
			linkWg.Add(1)
			go linkWorker(links, quit, &linkWg)
		}

		filepath.Walk(source, func(path string, f os.FileInfo, err error) error {
			// ignore the node_modules dir in the root, but lead sidecar
			if f.Name() == "node_modules" && strings.Contains(path, "sugarcrm/node_modules") {
				return filepath.SkipDir
			}
			if !f.IsDir() {
				builtFiles.Increment()
				// handle symlinks differently than normal files
				if f.Mode()&os.ModeSymlink != 0 {
					originFile, _ := os.Readlink(path)
					links <- Link{Link: path, Target: originFile}
				} else {
					files <- File(path)
				}
			}
			return nil
		})

		// end of tasks. the workers should quit afterwards
		close(files)
		close(links)
		// use "close(quit)", if you do not want to wait for the remaining tasks

		// wait for all workers to shut down properly
		wg.Wait()
		linkWg.Wait()

		fmt.Printf("Built %d files", builtFiles.Get())
	},
}

func init() {
	RootCmd.AddCommand(buildCmd)

	buildCmd.Flags().StringVarP(&destination,"destination", "d", "", "Where should the built files be put")
	buildCmd.Flags().StringVarP(&version, "version", "v", "","What Version is being built")
	buildCmd.Flags().StringVarP(&flavor, "flavor", "f", "ent","What Flavor of SugarCRM to build")
	buildCmd.Flags().BoolVar(&clean, "clean", false, "Remove Existing Build Before Building")

	buildCmd.Flags().IntVar(&fileWorkers, "file-workers", 40, "Number of Workers to start for processing files")
	buildCmd.Flags().IntVar(&fileBufferSize, "file-buffer-size", 4096, "Size of the file buffer before it gets reset")

	buildCmd.Flags().IntVar(&linkWorkers, "symlink-workers", 5, "Number of workers to start for processing symlinks")
	buildCmd.Flags().IntVar(&linkBufferSize, "symlink-buffer-size", 2048, "Size of the symlink buffer before it gets reset")

	buildCmd.MarkFlagRequired("version")
	buildCmd.MarkFlagRequired("flavor")
	buildCmd.MarkFlagRequired("destination")

}

// exists returns whether the given file or directory exists or not
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil { return true, nil }
	if os.IsNotExist(err) { return false, nil }
	return true, err
}

func fileWorker(files <-chan File, quit <-chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case file, ok := <-files:
			if !ok {
				return
			}
			shortPath := strings.Replace(string(file), source, "", -1)
			finalDestination := destination + string(filepath.Separator) + shortPath
			build.BuildFile(string(file), finalDestination, flavor, version)
		case <-quit:
			return
		}
	}
}

func linkWorker(links <- chan Link, quit <- chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case link, ok := <-links:
			if !ok {
				return
			}
			shortPath := strings.Replace(string(link.Link), source, "", -1)
			finalDestination := destination + string(filepath.Separator) + shortPath
			os.MkdirAll(path.Dir(finalDestination), 0775)
			os.Symlink(link.Target, destination)
		case <-quit:
			return
		}
	}
}
