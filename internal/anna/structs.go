package anna

import (
	"encoding/json"
	"fmt"
)

type Book struct {
	Language  string `json:"language"`
	Format    string `json:"format"`
	Size      string `json:"size"`
	Title     string `json:"title"`
	Publisher string `json:"publisher"`
	Authors   string `json:"authors"`
	URL       string `json:"url"`
	Hash      string `json:"hash"`
}

type Paper struct {
	DOI         string `json:"doi"`
	Title       string `json:"title,omitempty"`
	Authors     string `json:"authors"`
	Journal     string `json:"journal"`
	Size        string `json:"size"`
	Hash        string `json:"hash,omitempty"`
	DownloadURL string `json:"download_url"`
	SciHubURL   string `json:"scihub_url,omitempty"`
	PageURL     string `json:"page_url"`
}

func (b *Book) String() string {
	return fmt.Sprintf("Title: %s\nAuthors: %s\nPublisher: %s\nLanguage: %s\nFormat: %s\nSize: %s\nURL: %s\nHash: %s",
		b.Title, b.Authors, b.Publisher, b.Language, b.Format, b.Size, b.URL, b.Hash)
}

func (b *Book) ToJSON() (string, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (p *Paper) String() string {
	return fmt.Sprintf("DOI: %s\nTitle: %s\nAuthors: %s\nJournal: %s\nSize: %s\nHash: %s\nDownload URL: %s\nSci-Hub: %s\nPage: %s",
		p.DOI, p.Title, p.Authors, p.Journal, p.Size, p.Hash, p.DownloadURL, p.SciHubURL, p.PageURL)
}
