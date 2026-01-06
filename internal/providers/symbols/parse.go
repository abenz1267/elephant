package symbols

import (
	"crypto/md5"
	"embed"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strings"
)

//go:embed data/*
var files embed.FS

type LDML struct {
	XMLName     xml.Name    `xml:"ldml"`
	Identity    Identity    `xml:"identity"`
	Annotations Annotations `xml:"annotations"`
}

type Identity struct {
	Version  Version  `xml:"version"`
	Language Language `xml:"language"`
}

type Version struct {
	Number string `xml:"number,attr"`
}

type Language struct {
	Type string `xml:"type,attr"`
}

type Annotations struct {
	Annotation []Annotation `xml:"annotation"`
}

type Annotation struct {
	CP   string `xml:"cp,attr"`
	Type string `xml:"type,attr,omitempty"`
	Text string `xml:",chardata"`
}

type Symbol struct {
	CP         string
	Searchable []string
}

var symbols = make(map[string]*Symbol)

func parse() {
	file, err := files.ReadFile(fmt.Sprintf("data/%s.xml", config.Locale))
	if err != nil {
		slog.Error(Name, "parsing", err)
		return
	}

	var ldml LDML

	err = xml.Unmarshal(file, &ldml)
	if err != nil {
		panic(err)
	}

	for _, v := range ldml.Annotations.Annotation {
		md5 := md5.Sum([]byte(v.CP))
		md5str := hex.EncodeToString(md5[:])

		if val, ok := symbols[md5str]; !ok {
			s := &Symbol{
				CP:         v.CP,
				Searchable: []string{},
			}

			if v.Type == "" {
				s.Searchable = append(s.Searchable, strings.Split(v.Text, "|")...)
			} else {
				s.Searchable = append(s.Searchable, v.Text)
			}

			symbols[md5str] = s
		} else {
			if v.Type == "" {
				val.Searchable = append(val.Searchable, strings.Split(v.Text, "|")...)
			} else {
				val.Searchable = append(val.Searchable, v.Text)
			}
		}
	}

	for _, v := range symbols {
		for n, m := range v.Searchable {
			v.Searchable[n] = strings.TrimSpace(m)
		}
	}
}
