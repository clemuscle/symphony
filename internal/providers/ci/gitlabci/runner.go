package gitlabci

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

func (p *Provider) EnsureRunner(name, executorType string) error {
	// Vérifier si le container runner tourne déjà
	out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", "symphony-gitlab-runner").Output()
	if err == nil && string(out) == "true\n" {
		log.Printf("runner container déjà actif")
		return nil
	}

	// Vérifier via API GitLab si un runner avec ce nom existe et est online
	data, _, err := p.api("GET", "/runners?type=instance_type&status=online", nil)
	if err != nil {
		return err
	}
	var runners []struct {
		ID          int    `json:"id"`
		Description string `json:"description"`
	}
	json.Unmarshal(data, &runners)
	for _, r := range runners {
		if r.Description == name {
			log.Printf("runner '%s' déjà enregistré (id: %d)", name, r.ID)
			return nil
		}
	}

	token := os.Getenv("GITLAB_RUNNER_TOKEN")
	if token == "" {
		return fmt.Errorf("GITLAB_RUNNER_TOKEN manquant")
	}

	return p.startRunnerContainer(name, token, executorType)
}

func (p *Provider) startRunnerContainer(name, token, executor string) error {
	// Stopper et supprimer si existe déjà (état inconsistant)
	exec.Command("docker", "stop", "symphony-gitlab-runner").Run()
	exec.Command("docker", "rm", "symphony-gitlab-runner").Run()

	// Lancer le container
	out, err := exec.Command("docker", "run", "-d",
		"--name", "symphony-gitlab-runner",
		"--restart", "always",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", "symphony-runner-config:/etc/gitlab-runner",
		"gitlab/gitlab-runner:latest",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %s — %w", string(out), err)
	}

	// Attendre que le container soit prêt
	time.Sleep(3 * time.Second)

	// Enregistrer UNE SEULE fois
	out, err = exec.Command("docker", "exec", "symphony-gitlab-runner",
		"gitlab-runner", "register",
		"--non-interactive",
		"--url", p.BaseURL,
		"--token", token,
		"--executor", executor,
		"--docker-image", "alpine:latest",
		"--docker-privileged",
		"--docker-volumes", "/var/run/docker.sock:/var/run/docker.sock",
		"--docker-network-mode", "host",
		"--description", name,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("register: %s — %w", string(out), err)
	}

	log.Printf("✅ runner '%s' lancé et enregistré", name)
	return nil
}
