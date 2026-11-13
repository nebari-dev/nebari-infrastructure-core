package local

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// kindCorefile is the Corefile kind ships (kind v0.31, CoreDNS 1.12).
const kindCorefile = `.:53 {
    errors
    health {
       lameduck 5s
    }
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
       pods insecure
       fallthrough in-addr.arpa ip6.arpa
       ttl 30
    }
    prometheus :9153
    forward . /etc/resolv.conf {
       max_concurrent 1000
    }
    cache 30 {
       disable success cluster.local
       disable denial cluster.local
    }
    loop
    reload
    loadbalance
}
`

const nebariRule = `    rewrite name regex ^(.*\.)?nebari\.lvh\.me\.$ nebari-gateway.envoy-gateway-system.svc.cluster.local. answer auto`

func TestWithPlatformDNS(t *testing.T) {
	withNebari := strings.Replace(kindCorefile, "    kubernetes cluster.local",
		"    "+platformDNSBegin+"\n"+nebariRule+"\n    "+platformDNSEnd+"\n    kubernetes cluster.local", 1)

	tests := []struct {
		name     string
		corefile string
		domain   string
		want     string
		wantErr  string
	}{
		{
			name:     "inserts the rule before the kubernetes plugin",
			corefile: kindCorefile,
			domain:   "nebari.lvh.me",
			want:     withNebari,
		},
		{
			name:     "leaves an up-to-date rule unchanged",
			corefile: withNebari,
			domain:   "nebari.lvh.me",
			want:     withNebari,
		},
		{
			name:     "replaces the rule when the domain changes",
			corefile: withNebari,
			domain:   "other.example.com",
			want: strings.Replace(kindCorefile, "    kubernetes cluster.local",
				"    "+platformDNSBegin+"\n"+
					`    rewrite name regex ^(.*\.)?other\.example\.com\.$ nebari-gateway.envoy-gateway-system.svc.cluster.local. answer auto`+
					"\n    "+platformDNSEnd+"\n    kubernetes cluster.local", 1),
		},
		{
			name:     "lowercases the domain",
			corefile: kindCorefile,
			domain:   "Nebari.LVH.me",
			want:     withNebari,
		},
		{
			name:     "removes the rule when the domain is empty",
			corefile: withNebari,
			domain:   "",
			want:     kindCorefile,
		},
		{
			name:     "leaves a config without the rule alone when the domain is empty",
			corefile: kindCorefile,
			domain:   "",
			want:     kindCorefile,
		},
		{
			name:     "fails without a kubernetes plugin",
			corefile: ".:53 {\n    forward . /etc/resolv.conf\n}\n",
			domain:   "nebari.lvh.me",
			wantErr:  "no kubernetes plugin",
		},
		{
			name:     "rejects a domain that could change the Corefile structure",
			corefile: kindCorefile,
			domain:   "nebari.lvh.me {\n}",
			wantErr:  "not a valid DNS name",
		},
		{
			name:     "fails on a begin marker without an end marker",
			corefile: strings.Replace(kindCorefile, "    ready\n", "    ready\n    "+platformDNSBegin+"\n", 1),
			domain:   "nebari.lvh.me",
			wantErr:  "without",
		},
		{
			name:     "fails on an end marker without a begin marker",
			corefile: strings.Replace(kindCorefile, "    ready\n", "    ready\n    "+platformDNSEnd+"\n", 1),
			domain:   "nebari.lvh.me",
			wantErr:  "without",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withPlatformDNS(tt.corefile, tt.domain)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("withPlatformDNS() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("withPlatformDNS() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("withPlatformDNS() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestEnsurePlatformDNS(t *testing.T) {
	corednsCM := func(corefile string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: corednsConfigMap, Namespace: corednsNamespace},
			Data:       map[string]string{corefileKey: corefile},
		}
	}

	tests := []struct {
		name        string
		objects     []*corev1.ConfigMap
		domain      string
		wantUpdates int
		wantRule    bool
		wantErr     bool
	}{
		{
			name:        "writes the rule into kind's Corefile",
			objects:     []*corev1.ConfigMap{corednsCM(kindCorefile)},
			domain:      "nebari.lvh.me",
			wantUpdates: 1,
			wantRule:    true,
		},
		{
			name:        "does not write when the rule is already there",
			objects:     []*corev1.ConfigMap{corednsCM(mustWithPlatformDNS(t, kindCorefile, "nebari.lvh.me"))},
			domain:      "nebari.lvh.me",
			wantUpdates: 0,
			wantRule:    true,
		},
		{
			name:        "does not write when there is no domain and no rule",
			objects:     []*corev1.ConfigMap{corednsCM(kindCorefile)},
			domain:      "",
			wantUpdates: 0,
		},
		{
			name:    "fails when the CoreDNS config is missing",
			domain:  "nebari.lvh.me",
			wantErr: true,
		},
		{
			name: "fails when the CoreDNS config has no Corefile",
			objects: []*corev1.ConfigMap{{
				ObjectMeta: metav1.ObjectMeta{Name: corednsConfigMap, Namespace: corednsNamespace},
				Data:       map[string]string{},
			}},
			domain:  "nebari.lvh.me",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			for _, cm := range tt.objects {
				if _, err := client.CoreV1().ConfigMaps(cm.Namespace).Create(context.Background(), cm, metav1.CreateOptions{}); err != nil {
					t.Fatalf("seed ConfigMap: %v", err)
				}
			}
			client.ClearActions()

			err := ensurePlatformDNS(context.Background(), client, tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ensurePlatformDNS() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ensurePlatformDNS() error = %v", err)
			}

			updates := 0
			for _, a := range client.Actions() {
				if a.GetVerb() == "update" {
					updates++
				}
			}
			if updates != tt.wantUpdates {
				t.Errorf("ConfigMap updates = %d, want %d", updates, tt.wantUpdates)
			}

			cm, err := client.CoreV1().ConfigMaps(corednsNamespace).Get(context.Background(), corednsConfigMap, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("read CoreDNS ConfigMap: %v", err)
			}
			if got := strings.Contains(cm.Data[corefileKey], nebariRule); got != tt.wantRule {
				t.Errorf("Corefile contains the rule = %v, want %v:\n%s", got, tt.wantRule, cm.Data[corefileKey])
			}
		})
	}
}

func mustWithPlatformDNS(t *testing.T, corefile, domain string) string {
	t.Helper()
	out, err := withPlatformDNS(corefile, domain)
	if err != nil {
		t.Fatalf("withPlatformDNS() error = %v", err)
	}
	return out
}
