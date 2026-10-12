package provider

import "strings"

// SplitImage sépare le dépôt et le tag d'une image. Le tag explicite
// l'emporte sur celui de l'image ; par défaut "latest". Le ":" d'un port de
// registry (gitlab.local:5050/...) n'est pas un tag.
func SplitImage(image, tag string) (string, string) {
	repo := image
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		repo = image[:i]
		if tag == "" {
			tag = image[i+1:]
		}
	}
	if tag == "" {
		tag = "latest"
	}
	return repo, tag
}
