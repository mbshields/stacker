package stacker

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/anuvu/stacker/lib"
	"github.com/anuvu/stacker/log"
	"github.com/cheggaaa/pb"
	"github.com/pkg/errors"
)

// download with caching support in the specified cache dir.
func Download(cacheDir string, url string, progress bool) (string, error) {
	name := path.Join(cacheDir, path.Base(url))

	if fi, err := os.Stat(name); err == nil {
		// File is found in cache
		// need to check if cache is valid before using it
		localHash, err := lib.HashFile(name, false)
		if err != nil {
			return "", err
		}
		localHash = strings.TrimPrefix(localHash, "sha256:")
		localSize := strconv.FormatInt(fi.Size(), 10)
		log.Debugf("Local file: hash: %s length: %s", localHash, localSize)

		remoteHash, remoteSize, err := getHttpFileInfo(url)
		if err != nil {
			// Needed for "working offline"
			// See https://github.com/anuvu/stacker/issues/44
			log.Infof("cannot obtain file info of %s, using cached copy", url)
			return name, nil
		}
		log.Debugf("Remote file: hash: %s length: %s", remoteHash, remoteSize)

		if localHash == remoteHash {
			// Cached file has same hash as the remote file
			log.Infof("matched hash of %s, using cached copy", url)
			return name, nil
		} else if localSize == remoteSize {
			// Cached file has same content length as the remote file
			log.Infof("matched content length of %s, taking a leap of faith and using cached copy", url)
			return name, nil
		}
		// Cached file has a different hash from the remote one
		// Need to cleanup
		err = os.RemoveAll(name)
		if err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		// File is not found in cache but there are other errors
		return "", err
	}

	// File is not in cache
	// it wasn't there in the first place or it was cleaned up
	out, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return "", err
	}
	defer out.Close()

	log.Infof("downloading %v", url)

	resp, err := http.Get(url)
	if err != nil {
		os.RemoveAll(name)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		os.RemoveAll(name)
		return "", errors.Errorf("couldn't download %s: %s", url, resp.Status)
	}

	source := resp.Body
	if progress {
		bar := pb.New(int(resp.ContentLength)).SetUnits(pb.U_BYTES)
		bar.ShowTimeLeft = true
		bar.ShowSpeed = true
		bar.Start()
		source = bar.NewProxyReader(source)
		defer bar.Finish()
	}

	_, err = io.Copy(out, source)
	return name, err
}

// getHttpFileInfo returns the hash and content size a file stored on a web server
func getHttpFileInfo(remoteURL string) (string, string, error) {

	// Verify URL scheme
	u, err := url.Parse(remoteURL)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", errors.Errorf("cannot obtain content info for non HTTP URL: (%s)", remoteURL)
	}

	// Make a HEAD call on remote URL
	resp, err := http.Head(remoteURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	// Get file info from header
	// If the hash is not present this is an empty string
	hash := resp.Header.Get("X-Checksum-Sha256")
	length := resp.Header.Get("Content-Length")

	return hash, length, nil
}
