// Package hostsfile gives the local site a name by editing the hosts file.
//
// http://inboxql.localhost:8420 is easier to remember and to bookmark than
// http://localhost:8420, and survives the port being one of several things a
// developer runs. The name points at this machine and nothing else.
//
// Everything InboxQL writes sits between two marker lines, so it can be found,
// replaced and removed without touching a single line anybody else wrote.
package hostsfile

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
)

const (
	beginMarker = "# >>> inboxql >>>"
	endMarker   = "# <<< inboxql <<<"
)

// Path is the hosts file for this platform. $INBOXQL_HOSTS_FILE replaces it,
// for tests above all.
func Path() string {
	if p := strings.TrimSpace(os.Getenv("INBOXQL_HOSTS_FILE")); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return root + `\System32\drivers\etc\hosts`
	}
	return "/etc/hosts"
}

var label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Check says whether name is a sensible name for the local site.
//
// Refused:
//   - .local, because macOS resolves it over multicast DNS before reading the
//     hosts file, and every page load would wait out that lookup first.
//   - anything that is not a hostname.
//
// Allowed with a warning: a name outside the suffixes reserved for exactly
// this — .localhost, .test, .internal, .home.arpa — because a name on a real
// top-level domain shadows that domain's real site on this machine.
func Check(name string) (warning string, err error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", errors.New("a name is required, such as inboxql.localhost")
	}
	if strings.HasSuffix(name, ".local") {
		return "", errors.New(".local is resolved by multicast DNS before the hosts file on macOS, " +
			"which makes every page wait — use inboxql.localhost instead")
	}
	for _, part := range strings.Split(name, ".") {
		if !label.MatchString(part) {
			return "", fmt.Errorf("%q is not a hostname", name)
		}
	}
	for _, safe := range []string{".localhost", ".test", ".internal", ".home.arpa"} {
		if strings.HasSuffix(name, safe) || name == strings.TrimPrefix(safe, ".") {
			return "", nil
		}
	}
	if !strings.Contains(name, ".") {
		return "", nil
	}
	return fmt.Sprintf("%s is on a real domain; on this machine it will hide that domain's real site. "+
		"inboxql.localhost is reserved for exactly this.", name), nil
}

// Block is what InboxQL writes for a name.
func Block(name string) string {
	return beginMarker + "\n" +
		"127.0.0.1\t" + name + "\n" +
		"::1\t" + name + "\n" +
		endMarker + "\n"
}

// Apply returns the hosts content with InboxQL's block replaced by one for
// name, or removed when name is empty. Everything outside the markers is kept
// byte for byte.
func Apply(content, name string) string {
	content = Strip(content)
	if name == "" {
		return content
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + Block(name)
}

// Strip removes InboxQL's block, and nothing else.
func Strip(content string) string {
	start := strings.Index(content, beginMarker)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], endMarker)
	if end < 0 {
		// A begin with no end is somebody's hand edit. Leave the file alone
		// rather than guess where the block stops.
		return content
	}
	end += start + len(endMarker)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:start] + content[end:]
}

// Current is the name InboxQL's block points at, or "".
func Current(content string) string {
	start := strings.Index(content, beginMarker)
	if start < 0 {
		return ""
	}
	for _, line := range strings.Split(content[start:], "\n")[1:] {
		if strings.HasPrefix(line, endMarker) {
			break
		}
		if f := strings.Fields(line); len(f) == 2 && f[0] == "127.0.0.1" {
			return f[1]
		}
	}
	return ""
}

// ErrPermission is returned when the hosts file cannot be written, which is
// the normal case for anyone who is not an administrator.
var ErrPermission = errors.New("the hosts file needs administrator rights to change")

// Write replaces the hosts file's InboxQL block.
func Write(name string) error {
	path := Path()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next := Apply(string(b), name)
	if next == string(b) {
		return nil
	}
	// Written in place rather than renamed over: the hosts file's owner,
	// mode and — on macOS — flags must survive, and a rename would replace
	// them with the writer's.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0o644)
	if err != nil {
		if os.IsPermission(err) {
			return ErrPermission
		}
		return err
	}
	defer f.Close()
	_, err = f.WriteString(next)
	return err
}

// Read returns the hosts file's content.
func Read() (string, error) {
	b, err := os.ReadFile(Path())
	return string(b), err
}
