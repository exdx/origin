package htpasswd

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/auth/user"
	"github.com/golang/glog"
	authapi "github.com/openshift/origin/pkg/auth/api"
	"github.com/openshift/origin/pkg/auth/authenticator"
)

// Authenticator watches a file generated by htpasswd to validate usernames and passwords
type Authenticator struct {
	providerName string
	file         string
	fileInfo     os.FileInfo
	mapper       authapi.UserIdentityMapper
	usernames    map[string]string
}

// New returns an authenticator which will validate usernames and passwords against the given htpasswd file
func New(providerName string, file string, mapper authapi.UserIdentityMapper) (authenticator.Password, error) {
	auth := &Authenticator{
		providerName: providerName,
		file:         file,
		mapper:       mapper,
	}
	if err := auth.loadIfNeeded(); err != nil {
		return nil, err
	}
	return auth, nil
}

func (a *Authenticator) AuthenticatePassword(username, password string) (user.Info, bool, error) {
	a.loadIfNeeded()

	if len(username) > 255 {
		username = username[:255]
	}
	if strings.Contains(username, ":") {
		return nil, false, errors.New("Usernames may not contain : characters")
	}
	hash, ok := a.usernames[username]
	if !ok {
		return nil, false, nil
	}
	if ok, err := testPassword(password, hash); !ok || err != nil {
		return nil, false, err
	}

	identity := authapi.NewDefaultUserIdentityInfo(a.providerName, username)
	user, err := a.mapper.UserFor(identity)
	glog.V(4).Infof("Got userIdentityMapping: %#v", user)
	if err != nil {
		return nil, false, fmt.Errorf("Error creating or updating mapping for: %#v due to %v", identity, err)
	}

	return user, true, nil

}

func (a *Authenticator) load() error {
	file, err := os.Open(a.file)
	if err != nil {
		return err
	}
	defer file.Close()

	newusernames := map[string]string{}
	warnedusernames := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if len(line) == 0 {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			glog.Warningf("Ignoring malformed htpasswd line: %s", line)
			continue
		}

		username := parts[0]
		password := parts[1]
		if _, duplicate := newusernames[username]; duplicate {
			if _, warned := warnedusernames[username]; !warned {
				warnedusernames[username] = true
				glog.Warningf("%s contains multiple passwords for user '%s'. The last one specified will be used.", a.file, username)
			}
		}
		newusernames[username] = password
	}

	a.usernames = newusernames

	return nil
}

func (a *Authenticator) loadIfNeeded() error {
	info, err := os.Stat(a.file)
	if err != nil {
		return err
	}
	if a.fileInfo == nil || a.fileInfo.ModTime() != info.ModTime() {
		glog.V(4).Infof("Loading htpasswd file %s...", a.file)
		a.fileInfo = info
		return a.load()
	}
	return nil
}

func testPassword(password, hash string) (bool, error) {
	switch {
	case strings.HasPrefix(hash, "$apr1$"):
		// MD5, default
		return testMD5Password(password, hash)
	case strings.HasPrefix(hash, "$2y$") || strings.HasPrefix(hash, "$2a$"):
		// Bcrypt, secure
		return testBCryptPassword(password, hash)
	case strings.HasPrefix(hash, "{SHA}"):
		// SHA-1, insecure
		return testSHAPassword(password, hash[5:])
	case len(hash) == 13:
		// looks like crypt
		return testCryptPassword(password, hash)
	default:
		return false, errors.New("Unrecognized hash type")
	}
}

func testSHAPassword(password, hash string) (bool, error) {
	if len(hash) == 0 {
		return false, errors.New("Invalid SHA hash")
	}
	// Compute hash of password
	shasum := sha1.Sum([]byte(password))
	// Base-64 encode
	base64shasum := base64.StdEncoding.EncodeToString(shasum[:])
	// Compare
	match := hash == base64shasum
	return match, nil
}

func testBCryptPassword(password, hash string) (bool, error) {
	// TODO: import bcrypt
	// err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	// if err == bcrypt.ErrMismatchedHashAndPassword {
	// 	return false, nil
	// }
	// if err != nil {
	// 	return false, err
	// }
	// return true, nil
	return false, errors.New("bcrypt password hashes are not supported")
}

func testMD5Password(password, hash string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 {
		return false, errors.New("Malformed MD5 hash")
	}

	salt := parts[2]
	if len(salt) == 0 {
		return false, errors.New("Malformed MD5 hash: missing salt")
	}
	if len(salt) > 8 {
		salt = salt[:8]
	}

	md5hash := parts[3]
	if len(md5hash) == 0 {
		return false, errors.New("Malformed MD5 hash: missing hash")
	}

	testhash := string(apr_md5([]byte(password), []byte(salt)))
	match := testhash == hash

	return match, nil
}

func testCryptPassword(password, hash string) (bool, error) {
	// if len(password) > 8 {
	// 	password = password[:8]
	// }
	// salt := hash[0:2]
	// hash = hash[2:]
	return false, errors.New("crypt password hashes are not supported")
}
