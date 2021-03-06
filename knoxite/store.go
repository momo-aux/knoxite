package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knoxite/knoxite"
	"github.com/muesli/goprogressbar"
)

// Error declarations
var (
	ErrRedundancyAmount = errors.New("failure tolerance can't be equal or higher as the number of storage backends")
)

// CmdStore describes the command
type CmdStore struct {
	Description      string `short:"d" long:"desc"        description:"a description or comment for this snapshot"`
	Compression      string `short:"c" long:"compression" description:"compression algo to use: none (default), gzip"`
	Encryption       string `short:"e" long:"encryption"  description:"encryption algo to use: aes (default), none"`
	FailureTolerance uint   `short:"t" long:"tolerance"   description:"failure tolerance against n backend failures"`

	global *GlobalOptions
}

func init() {
	_, err := parser.AddCommand("store",
		"store file/directory",
		"The store command creates a snapshot of a file or directory",
		&CmdStore{global: &globalOpts})
	if err != nil {
		panic(err)
	}
}

func (cmd CmdStore) store(repository *knoxite.Repository, snapshot *knoxite.Snapshot, targets []string) error {
	fmt.Println()
	overallProgressBar := goprogressbar.NewProgressBar("Overall Progress", 0, 0, 60)
	wd, gerr := os.Getwd()
	if gerr != nil {
		return gerr
	}

	if uint(len(repository.Backend.Backends))-cmd.FailureTolerance <= 0 {
		return ErrRedundancyAmount
	}

	progress, serr := snapshot.Add(wd, targets, *repository,
		strings.ToLower(cmd.Compression) == "gzip", strings.ToLower(cmd.Encryption) != "none",
		uint(len(repository.Backend.Backends))-cmd.FailureTolerance, cmd.FailureTolerance)
	if serr != nil {
		return serr
	}

	fileProgressBar := goprogressbar.NewProgressBar("", 0, 0, 60)
	lastPath := ""
	for p := range progress {
		if p.Path != lastPath && lastPath != "" {
			fmt.Println()
		}
		fileProgressBar.Total = int64(p.Size)
		fileProgressBar.Current = int64(p.StorageSize)
		fileProgressBar.RightAlignedText = fmt.Sprintf("%s / %s",
			knoxite.SizeToString(uint64(fileProgressBar.Current)),
			knoxite.SizeToString(uint64(fileProgressBar.Total)))

		overallProgressBar.Total = int64(p.Statistics.Size)
		overallProgressBar.Current = int64(p.Statistics.StorageSize)
		overallProgressBar.RightAlignedText = fmt.Sprintf("%s / %s",
			knoxite.SizeToString(uint64(overallProgressBar.Current)),
			knoxite.SizeToString(uint64(overallProgressBar.Total)))

		if p.Path != lastPath {
			lastPath = p.Path
			fileProgressBar.Text = p.Path
		}

		goprogressbar.MoveCursorUp(1)
		fileProgressBar.Print()
		goprogressbar.MoveCursorDown(1)
		overallProgressBar.Print()
	}

	fmt.Printf("\nSnapshot %s created: %s\n", snapshot.ID, snapshot.Stats.String())
	return nil
}

// Usage describes this command's usage help-text
func (cmd CmdStore) Usage() string {
	return "VOLUME-ID DIR/FILE [DIR/FILE] [...]"
}

// Execute this command
func (cmd CmdStore) Execute(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf(TWrongNumArgs, cmd.Usage())
	}
	if cmd.global.Repo == "" {
		return ErrMissingRepoLocation
	}

	targets := []string{}
	for _, target := range args[1:] {
		if absTarget, err := filepath.Abs(target); err == nil {
			target = absTarget
		}
		targets = append(targets, target)
	}

	// filter here? exclude/include?

	repository, err := openRepository(cmd.global.Repo, cmd.global.Password)
	if err != nil {
		return err
	}
	volume, err := repository.FindVolume(args[0])
	if err != nil {
		return err
	}
	snapshot, err := knoxite.NewSnapshot(cmd.Description)
	if err != nil {
		return err
	}
	err = cmd.store(&repository, &snapshot, targets)
	if err != nil {
		return err
	}
	err = snapshot.Save(&repository)
	if err != nil {
		return err
	}
	err = volume.AddSnapshot(snapshot.ID)
	if err != nil {
		return err
	}
	return repository.Save()
}
