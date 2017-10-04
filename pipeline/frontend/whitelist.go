package frontend

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/boltdb/bolt"
	"github.com/google/go-github/github"
)

var (
	databaseName = "whitelist.db"
	bucketName   = "active-repos"
	whitelist    = "whitelist.toml"
)

type User struct {
	Owner string
	Repo  string
}

type Config struct {
	Title   string
	Maximum int
	User    []User
}

func checkTOML(users []User, owner, repo string) bool {
	for _, user := range users {
		if user.Owner == owner && user.Repo == repo {
			return true
		}
	}
	return false
}

func (fs *FrontendServer) CheckWhitelist(repo github.Repository) (string, error) {
	boltDB, err := bolt.Open(databaseName, 0644, nil)
	if err != nil {
		return "", err
	}
	defer boltDB.Close()

	output := ""
	err = boltDB.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}
		count := bucket.Stats().KeyN
		config := Config{}
		if _, err := toml.DecodeFile(whitelist, &config); err != nil {
			return err
		}

		repoOwner := *repo.Owner.Login
		repoName := *repo.Name
		if count <= config.Maximum || checkTOML(config.User, repoOwner, repoName) {
			content := []string{repoOwner, repoName}
			buf := &bytes.Buffer{}
			gob.NewEncoder(buf).Encode(content)
			info := buf.Bytes()
			if err := bucket.Put([]byte(bucketName), info); err != nil {
				return err
			}
		}
		output = fmt.Sprintf("%v/%v", repoOwner, repoName)
		return nil
	})
	return output, err
}
