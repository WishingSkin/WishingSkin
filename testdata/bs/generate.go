// Run with: go run ./testdata/bs/generate.go <new-output-directory>
// The generated private key is for a local synthetic fixture only.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/WishingSkin/WishingSkin/internal/bsmigration"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: go run ./testdata/bs/generate.go <new-output-directory> (from repository root)")
	}
	s, err := bsmigration.Load("testdata/bs/snapshot.json")
	must(err)
	s.FormatVersion = bsmigration.FormatVersion
	verified := false
	s.Users[0].Verified = &verified
	s.Textures[0].Uploader = &s.Users[0].ID
	s.Textures[0].Description = "Synthetic migrated description"
	alias := "Synthetic collection"
	s.Wardrobe = []bsmigration.Collection{{UserID: s.Users[0].ID, TextureID: s.Textures[0].ID, Name: &alias}}
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	must(err)
	s.SignaturePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	dir, err := filepath.Abs(os.Args[1])
	must(err)
	err = os.Mkdir(dir, 0700)
	must(err)
	data, err := json.MarshalIndent(s, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(dir, "snapshot.json"), append(data, '\n'), 0600))
	for _, t := range s.Textures {
		b, err := os.ReadFile(filepath.Join("testdata", "bs", t.Hash))
		must(err)
		must(os.WriteFile(filepath.Join(dir, t.Hash), b, 0600))
	}
	// SQL is a synthetic source seed for a separate local MySQL database, never
	// consumed or executed by the exporter/importer itself.
	quote := func(v string) string {
		return "'" + strings.ReplaceAll(strings.ReplaceAll(v, "\\", "\\\\"), "'", "''") + "'"
	}
	u, c, t := s.Users[0], s.Characters[0], s.Textures[0]
	sql := `CREATE TABLE users(uid INT PRIMARY KEY,email VARCHAR(100),nickname VARCHAR(50),password VARCHAR(255),score INT,permission INT,verified TINYINT);
CREATE TABLE players(pid INT PRIMARY KEY,uid INT,name VARCHAR(50),tid_skin INT,tid_cape INT);
CREATE TABLE textures(tid INT PRIMARY KEY,name VARCHAR(50),type VARCHAR(10),hash VARCHAR(64),public TINYINT,uploader INT);
CREATE TABLE uuid(id INT PRIMARY KEY,name VARCHAR(255),uuid VARCHAR(255));
CREATE TABLE options(id INT PRIMARY KEY,option_name VARCHAR(50),option_value LONGTEXT);
CREATE TABLE user_closet(user_uid INT,texture_tid INT,item_name VARCHAR(50));
CREATE TABLE textures_description(id INT PRIMARY KEY,tid INT,description VARCHAR(255));
`
	sql += fmt.Sprintf("INSERT INTO users VALUES(%d,%s,%s,%s,%d,%d,0);\n", u.ID, quote(u.Email), quote(u.Nickname), quote(u.PasswordHash), u.Score, u.Permission)
	sql += fmt.Sprintf("INSERT INTO players VALUES(%d,%d,%s,%d,%d);\n", c.ID, c.UserID, quote(c.Name), c.SkinID, c.CapeID)
	sql += fmt.Sprintf("INSERT INTO textures VALUES(%d,%s,%s,%s,0,%d);\n", t.ID, quote(t.Name), quote(t.Type), quote(t.Hash), *t.Uploader)
	sql += fmt.Sprintf("INSERT INTO user_closet VALUES(%d,%d,%s);\n", u.ID, t.ID, quote(alias))
	sql += fmt.Sprintf("INSERT INTO textures_description VALUES(1,%d,%s);\n", t.ID, quote(t.Description))
	sql += fmt.Sprintf("INSERT INTO uuid VALUES(1,%s,%s);\n", quote(c.Name), quote(c.UUID))
	sql += "INSERT INTO options VALUES(1,'ygg_private_key'," + quote(s.SignaturePrivateKey) + "),(2,'ygg_uuid_algorithm','v4');\n"
	must(os.WriteFile(filepath.Join(dir, "source.sql"), []byte(sql), 0600))
	fmt.Println("Synthetic fixture generated at", dir, "; password: password; source SQL must only be loaded into an empty local fixture database.")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
