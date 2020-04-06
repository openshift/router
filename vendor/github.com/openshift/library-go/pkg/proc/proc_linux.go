package proc

import (
	"bufio"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"k8s.io/klog"
)

// parseProcForZombies parses the current procfs mounted at /proc
// to find processes in the zombie state.
func parseProcForZombies() ([]int, error) {
	files, err := ioutil.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	var zombies []int
	for _, file := range files {
		processID, err := strconv.Atoi(file.Name())
		if err != nil {
			break
		}
		stateFilePath := filepath.Join("/proc", file.Name(), "status")
		fd, err := os.Open(stateFilePath)
		if err != nil {
			klog.V(4).Infof("Failed to open %q: %v", stateFilePath, err)
			continue
		}
		defer fd.Close()
		fs := bufio.NewScanner(fd)
		for fs.Scan() {
			line := fs.Text()
			if strings.HasPrefix(line, "State:") {
				if strings.Contains(line, "zombie") {
					zombies = append(zombies, processID)
				}
				break
			}
		}
	}

	return zombies, nil
}

// StartReaper starts a goroutine to reap processes periodically if called
// from a pid 1 process.
func StartReaper(period time.Duration) {
	if os.Getpid() == 1 {
		go func() {
			var zs []int
			var err error
			for {
				zs, err = parseProcForZombies()
				if err != nil {
					klog.V(4).Infof(err.Error())
					continue
				}
				time.Sleep(period)
				for _, z := range zs {
					klog.V(4).Infof("Reaping zombie: %d", z)
					cpid, err := syscall.Wait4(z, nil, syscall.WNOHANG, nil)
					if err != nil {
						klog.V(4).Infof("Zombie reap error: %v", err)
					} else {
						klog.V(4).Infof("Zombie reaped: %d", cpid)
					}
				}
			}
		}()
	}
}
