package notify

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type channel struct {
	id  string
	mu  sync.Mutex
	got []provider.Notification
	err error
}

func (c *channel) ID() string             { return c.id }
func (c *channel) Type() string           { return "test" }
func (c *channel) Check() (string, error) { return "", nil }
func (c *channel) Notify(n provider.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, n)
	return c.err
}

func TestSend(t *testing.T) {
	payments, platform, broken := &channel{id: "payments"}, &channel{id: "platform"}, &channel{id: "broken", err: errors.New("410")}
	providers := provider.NewRegistry()
	for _, c := range []*channel{payments, platform, broken} {
		providers.RegisterNotifications(c)
	}
	d := New(providers, &Settings{
		URL:     "https://symphony.test/",
		Teams:   map[string][]string{"team-payments": {"payments", "broken"}},
		Default: []string{"platform"},
	})
	sent := make(chan string, 10)
	d.sent = func(id string, err error) { sent <- id }

	d.Send("team-payments", provider.Notification{Title: "a"})
	d.Send("team-identity", provider.Notification{Title: "b"}) // sans canal : ceux par défaut
	d.Send("", provider.Notification{Title: "c"})
	for i := 0; i < 4; i++ {
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
			t.Fatal("envoi manquant")
		}
	}
	if len(payments.got) != 1 || len(broken.got) != 1 || len(platform.got) != 2 {
		t.Errorf("envois : payments %v, broken %v, platform %v", payments.got, broken.got, platform.got)
	}
	if l := d.Link("/tasks"); l != "https://symphony.test/tasks" {
		t.Errorf("lien %q", l)
	}

	// Sans section notifications : rien, pas de lien
	d.Set(nil)
	d.Send("team-payments", provider.Notification{Title: "d"})
	if d.Link("/tasks") != "" || len(d.Channels("team-payments")) != 0 {
		t.Error("notifications sans configuration")
	}
	var none *Dispatcher
	none.Send("x", provider.Notification{}) // pas de panique
}
