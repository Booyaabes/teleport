// Copyright 2022 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package helpers

import (
	"context"
	"crypto/x509/pkix"
	"net"
	"path/filepath"
	"testing"
	"time"

	apiclient "github.com/gravitational/teleport/api/client"
	"github.com/gravitational/teleport/api/client/proto"
	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/auth/testauthority"
	"github.com/gravitational/teleport/lib/client"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/kube/kubeconfig"
	"github.com/gravitational/teleport/lib/service"
	"github.com/gravitational/teleport/lib/sshutils"
	"github.com/gravitational/teleport/lib/tlsca"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func EnableKubernetesService(t *testing.T, config *service.Config) {
	config.Kube.KubeconfigPath = filepath.Join(t.TempDir(), "kube_config")
	require.NoError(t, EnableKube(config, "teleport-cluster"))
}

func EnableKube(config *service.Config, clusterName string) error {
	kubeConfigPath := config.Kube.KubeconfigPath
	if kubeConfigPath == "" {
		return trace.BadParameter("missing kubeconfig path")
	}
	key, err := genUserKey()
	if err != nil {
		return trace.Wrap(err)
	}
	err = kubeconfig.Update(kubeConfigPath, kubeconfig.Values{
		TeleportClusterName: clusterName,
		ClusterAddr:         "https://" + net.JoinHostPort(Host, ports.Pop()),
		Credentials:         key,
	})
	if err != nil {
		return trace.Wrap(err)
	}
	config.Kube.Enabled = true
	config.Kube.ListenAddr = utils.MustParseAddr(net.JoinHostPort(Host, ports.Pop()))
	return nil
}

// GetKubeClusters gets all kubernetes clusters accessible from a given auth server.
func GetKubeClusters(t *testing.T, as *auth.Server) []*types.KubernetesCluster {
	ctx := context.Background()
	resources, err := apiclient.GetResourcesWithFilters(ctx, as, proto.ListResourcesRequest{
		ResourceType: types.KindKubeService,
	})
	require.NoError(t, err)
	kss, err := types.ResourcesWithLabels(resources).AsServers()
	require.NoError(t, err)

	clusters := make([]*types.KubernetesCluster, 0)
	for _, ks := range kss {
		clusters = append(clusters, ks.GetKubernetesClusters()...)
	}
	return clusters
}

func genUserKey() (*client.Key, error) {
	caKey, caCert, err := tlsca.GenerateSelfSignedCA(pkix.Name{
		CommonName:   "localhost",
		Organization: []string{"localhost"},
	}, nil, defaults.CATTL)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	ca, err := tlsca.FromKeys(caCert, caKey)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	keygen := testauthority.New()
	priv, pub, err := keygen.GenerateKeyPair()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	cryptoPub, err := sshutils.CryptoPublicKey(pub)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	clock := clockwork.NewRealClock()
	tlsCert, err := ca.GenerateCertificate(tlsca.CertificateRequest{
		Clock:     clock,
		PublicKey: cryptoPub,
		Subject: pkix.Name{
			CommonName: "teleport-user",
		},
		NotAfter: clock.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &client.Key{
		Priv:    priv,
		Pub:     pub,
		TLSCert: tlsCert,
		TrustedCA: []auth.TrustedCerts{{
			TLSCertificates: [][]byte{caCert},
		}},
	}, nil
}
