package archlinuxpkgs

//go:generate msgp
type CachedData struct {
	Packages map[string]Package
}

func newCachedData() CachedData {
	return CachedData{
		Packages: make(map[string]Package),
	}
}

//go:generate msgp
type Package struct {
	Name           string
	Description    string
	Repository     string
	Version        string
	Installed      bool
	FullInfo       string
	URL            string
	URLPath        string
	Maintainer     string
	Submitter      string
	NumVotes       int
	Popularity     float64
	FirstSubmitted int64
	LastModified   int64
}
