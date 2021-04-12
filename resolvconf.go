// Package resolvconf provides utility code to query and update DNS configuration in /etc/resolv.conf
package resolvconf

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"sync"
)

const (
	// defaultPath is the default path to the resolv.conf that contains information to resolve DNS. See Path().
	defaultPath = "/etc/resolv.conf"
	// alternatePath is a path different from defaultPath, that may be used to resolve DNS. See Path().
	alternatePath = "/run/systemd/resolve/resolv.conf"

	commentMark = "#"
)

var (
	detectSystemdResolvConfOnce sync.Once
	pathAfterSystemdDetection   = defaultPath
)

// Path returns the path to the resolv.conf file that libnetwork should use.
//
// When /etc/resolv.conf contains 127.0.0.53 as the only nameserver, then
// it is assumed systemd-resolved manages DNS. Because inside the container 127.0.0.53
// is not a valid DNS server, Path() returns /run/systemd/resolve/resolv.conf
// which is the resolv.conf that systemd-resolved generates and manages.
// Otherwise Path() returns /etc/resolv.conf.
//
// Errors are silenced as they will inevitably resurface at future open/read calls.
//
// More information at https://www.freedesktop.org/software/systemd/man/systemd-resolved.service.html#/etc/resolv.conf
func Path() string {
	detectSystemdResolvConfOnce.Do(func() {
		candidateResolvConf, err := ioutil.ReadFile(defaultPath)
		if err != nil {
			// silencing error as it will resurface at next calls trying to read defaultPath
			return
		}
		ns, err := getNameservers(string(candidateResolvConf))
		if err != nil {
			// same as ignoring error upper
			return
		}

		if len(ns) == 1 && ns[0].IsLoopback() {
			pathAfterSystemdDetection = alternatePath
		}
	})
	return pathAfterSystemdDetection
}


// File contains the resolv.conf content and its hash
// todo: make https://linux.die.net/man/5/resolv.conf full spec-compilant
type File struct {
	Content []byte
	Hash    string

	Nameservers []net.IP
	Options     []string // todo: options are fixed, need to make options type and a few methods for it
}

// Get returns the contents of /etc/resolv.conf and its hash
func Get() (*File, error) {
	return GetSpecific(Path())
}

// GetSpecific returns the contents of the user specified resolv.conf file and its hash
func GetSpecific(path string) (*File, error) {
	resolv, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hash, err := hashData(bytes.NewReader(resolv))
	if err != nil {
		return nil, err
	}

	nameservers, err := getNameservers(string(resolv))
	if err != nil {
		return nil, err
	}

	options := getOptions(string(resolv))

	return &File{
		Content:     resolv,
		Hash:        hash,
		Nameservers: nameservers,
		Options: options,
	}, nil
}

const nameserverKey = "nameserver"

// getNameservers returns nameservers (if any) listed in /etc/resolv.conf
func getNameservers(resolvConf string) ([]net.IP, error) {
	nameservers := []net.IP{}
	for i, line := range getLines(resolvConf, commentMark) {
		if !strings.HasPrefix(line, nameserverKey) {
			continue // skip if not nameserver
		}

		line := strings.TrimSpace(strings.TrimPrefix(line, nameserverKey))
		ip := net.ParseIP(line)
		if ip == nil {
			return nil, fmt.Errorf("line %v: invalid ip address of nameserver: %q", i, line)
		}

		nameservers = append(nameservers, ip)
	}
	return nameservers, nil
}

const optionKey = "option"

// GetOptions returns options (if any) listed in /etc/resolv.conf
// If more than one options line is encountered, only the contents of the last
// one is returned.
func getOptions(resolvConf string) []string {
	options := []string{}
	for _, line := range getLines(resolvConf, commentMark) {
		if !strings.HasPrefix(line, optionKey) {
			continue // skip if not option
		}

		line := strings.TrimSpace(strings.TrimPrefix(line, nameserverKey))
		options = append(options, line)
	}
	return options
}

// getLines parses input into lines and strips away comments and spaces.
func getLines(input string, commentMarker string) []string {
	lines := strings.Split(input, "\n")
	output := make([]string, 0, len(lines)) // hope that count of comments is 1 or 2 lines
	for _, currentLine := range lines {
		var commentIndex = strings.Index(currentLine, commentMarker)
		line := currentLine
		if commentIndex != -1 {
			line = line[:commentIndex]
		}

		output = append(output, strings.TrimSpace(line))
	}
	return output
}
