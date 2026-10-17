package argocd

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

// TestCNPGOnCluster : l'infra d'une stack (un PostgreSQL CloudNativePG),
// rendue par le runtime puis appliquée sur un vrai cluster avec l'opérateur,
// comme le ferait Argo CD : la base démarre, son secret porte l'URI que l'appli
// lit, on s'y connecte, puis tout part avec elle. Lancé par la CI sur kind
// (SYMPHONY_TEST_K8S=1, kubectl pointé sur le cluster).
func TestCNPGOnCluster(t *testing.T) {
	if os.Getenv("SYMPHONY_TEST_K8S") != "1" {
		t.Skip("SYMPHONY_TEST_K8S=1 et un cluster avec CloudNativePG requis")
	}
	ns := fmt.Sprintf("symphony-it-%d", time.Now().Unix()%100000)
	kubectl := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("kubectl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("kubectl %s : %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	kubectl("create", "namespace", ns)
	t.Cleanup(func() { exec.Command("kubectl", "delete", "namespace", ns, "--wait=false").Run() })

	p, scm, _ := newTest(t)
	name := "api-staging"
	if _, err := p.Deploy(provider.DeployRequest{Name: name, Image: "nginx", Tag: "1", Infra: testInfra}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for f, c := range scm.files {
		path := filepath.Join(dir, strings.TrimPrefix(f, "apps/"+name+"/"))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(c), 0o644)
	}
	// L'infra seule : l'appli de test n'a pas d'image qui parle à PostgreSQL
	kubectl("-n", ns, "apply", "-k", filepath.Join(dir, infraDir))

	db := name + "-db"
	kubectl("-n", ns, "wait", "cluster/"+db, "--for=condition=Ready", "--timeout=300s")
	raw := kubectl("-n", ns, "get", "secret", db+"-app", "-o", "jsonpath={.data.uri}")
	uri, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || !strings.HasPrefix(string(uri), "postgresql://app:") {
		t.Fatalf("URI de la base : %q, %v", uri, err)
	}
	// Le Deployment généré lit cette URI dans ce secret
	if dep := scm.files["apps/"+name+"/deployment.yaml"]; !strings.Contains(dep, `name: "`+db+`-app"`) {
		t.Errorf("deployment.yaml ne lit pas %s-app :\n%s", db, dep)
	}
	out := kubectl("-n", ns, "exec", db+"-1", "-c", "postgres", "--", "psql", string(uri), "-tAc", "select 42")
	if strings.TrimSpace(out) != "42" {
		t.Errorf("requête : %q", out)
	}

	// Supprimer l'infra (ce que fait Argo CD en supprimant l'Application en
	// cascade) emporte la base et ses volumes
	kubectl("-n", ns, "delete", "-k", filepath.Join(dir, infraDir), "--wait=true", "--timeout=120s")
	for i := 0; i < 30; i++ {
		if strings.TrimSpace(kubectl("-n", ns, "get", "pvc", "-o", "name")) == "" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Errorf("volumes restants : %s", kubectl("-n", ns, "get", "pvc"))
}
