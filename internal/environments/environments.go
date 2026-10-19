// Package environments : les environnements de déploiement (staging,
// production…) déclarés par l'équipe plateforme dans la configuration, dans
// l'ordre de promotion. Un environnement protégé ne reçoit que des promotions :
// une image déjà déployée et saine dans l'environnement précédent. Un
// environnement avec approbation (approval: lead) fait attendre le déploiement
// demandé par un developer jusqu'à l'accord d'un lead de l'équipe du projet.
package environments

import (
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"
)

type Environment struct {
	Name      string `yaml:"name" json:"name"`
	Runtime   string `yaml:"runtime" json:"runtime"` // runtime par défaut ; une stack peut le changer
	Protected bool   `yaml:"protected" json:"protected"`
	// Approval : "lead" (un lead de l'équipe ou un admin approuve chaque
	// déploiement demandé par un developer), "none" ou vide : aucune
	Approval string `yaml:"approval" json:"approval"`
}

// ApprovalLead : les déploiements attendent l'accord d'un lead
const ApprovalLead = "lead"

// NeedsApproval : un déploiement dans e attend un accord
func (e Environment) NeedsApproval() bool { return e.Approval == ApprovalLead }

// nameRe : court et DNS, le nom finit dans celui des déploiements (<projet>-<env>)
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,19}$`)

// Validate vérifie une liste d'environnements : noms DNS uniques, runtime
// existant (runtimeExists), et un premier environnement non protégé (une
// promotion part toujours de l'environnement précédent)
func Validate(envs []Environment, runtimeExists func(string) bool) error {
	var errs []error
	seen := map[string]bool{}
	for i, e := range envs {
		where := fmt.Sprintf("environments[%d]", i)
		if e.Name != "" {
			where = "environments " + e.Name
		}
		if !nameRe.MatchString(e.Name) {
			errs = append(errs, fmt.Errorf("%s : nom invalide (minuscules, chiffres, tirets, 20 caractères max.)", where))
		}
		if seen[e.Name] {
			errs = append(errs, fmt.Errorf("%s : nom en double", where))
		}
		seen[e.Name] = true
		if !runtimeExists(e.Runtime) {
			errs = append(errs, fmt.Errorf("%s : runtime %q inconnu", where, e.Runtime))
		}
		if e.Approval != "" && e.Approval != "none" && e.Approval != ApprovalLead {
			errs = append(errs, fmt.Errorf("%s : approval %q inconnu (lead, none)", where, e.Approval))
		}
		if i == 0 && e.Protected {
			errs = append(errs, fmt.Errorf("%s : le premier environnement ne peut pas être protégé (rien à promouvoir)", where))
		}
	}
	return errors.Join(errs...)
}

// Holder porte la liste courante, remplacée au rechargement de la configuration
type Holder struct{ p atomic.Pointer[[]Environment] }

func NewHolder(envs []Environment) *Holder {
	h := &Holder{}
	h.Set(envs)
	return h
}

func (h *Holder) Set(envs []Environment) {
	if envs == nil {
		envs = []Environment{}
	}
	h.p.Store(&envs)
}

// List : les environnements, dans l'ordre de promotion (vide : aucun
// environnement, un déploiement par projet comme avant)
func (h *Holder) List() []Environment { return *h.p.Load() }

// Find : l'environnement name et sa position
func (h *Holder) Find(name string) (Environment, int, bool) {
	for i, e := range h.List() {
		if e.Name == name {
			return e, i, true
		}
	}
	return Environment{}, -1, false
}

// Previous : l'environnement d'où l'on promeut vers name
func (h *Holder) Previous(name string) (Environment, bool) {
	_, i, ok := h.Find(name)
	if !ok || i == 0 {
		return Environment{}, false
	}
	return h.List()[i-1], true
}
