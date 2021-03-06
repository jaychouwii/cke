package k8s

import (
	"bytes"
	"time"

	"github.com/cybozu-go/cke"
	"github.com/cybozu-go/cke/op"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8sjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	apiserverv1 "k8s.io/apiserver/pkg/apis/config/v1"
	"k8s.io/client-go/tools/clientcmd/api"
	schedulerv1beta1 "k8s.io/kube-scheduler/config/v1beta1"
	kubeletv1beta1 "k8s.io/kubelet/config/v1beta1"
	"k8s.io/utils/pointer"
)

var (
	resourceEncoder runtime.Encoder
	scm             = runtime.NewScheme()
)

func init() {
	if err := apiserverv1.AddToScheme(scm); err != nil {
		panic(err)
	}
	if err := kubeletv1beta1.AddToScheme(scm); err != nil {
		panic(err)
	}
	if err := schedulerv1beta1.AddToScheme(scm); err != nil {
		panic(err)
	}
	resourceEncoder = k8sjson.NewSerializerWithOptions(k8sjson.DefaultMetaFactory, scm, scm,
		k8sjson.SerializerOptions{Yaml: true})
}

func encodeToYAML(obj runtime.Object) ([]byte, error) {
	unst := &unstructured.Unstructured{}
	if err := scm.Convert(obj, unst, nil); err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	if err := resourceEncoder.Encode(unst, buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func controllerManagerKubeconfig(cluster string, ca, clientCrt, clientKey string) *api.Config {
	return cke.Kubeconfig(cluster, "system:kube-controller-manager", ca, clientCrt, clientKey)
}

func schedulerKubeconfig(cluster string, ca, clientCrt, clientKey string) *api.Config {
	return cke.Kubeconfig(cluster, "system:kube-scheduler", ca, clientCrt, clientKey)
}

// GenerateSchedulerConfiguration generates scheduler configuration.
// `params` must be validated beforehand.
func GenerateSchedulerConfiguration(params cke.SchedulerParams) *schedulerv1beta1.KubeSchedulerConfiguration {
	// default values
	base := schedulerv1beta1.KubeSchedulerConfiguration{}

	c, err := params.MergeConfig(&base)
	if err != nil {
		panic(err)
	}

	// forced values
	c.ClientConnection.Kubeconfig = op.SchedulerKubeConfigPath
	c.LeaderElection.LeaderElect = pointer.BoolPtr(true)

	return c
}

func proxyKubeconfig(cluster string, ca, clientCrt, clientKey string) *api.Config {
	return cke.Kubeconfig(cluster, "system:kube-proxy", ca, clientCrt, clientKey)
}

func kubeletKubeconfig(cluster string, n *cke.Node, caPath, certPath, keyPath string) *api.Config {
	cfg := api.NewConfig()
	c := api.NewCluster()
	c.Server = "https://localhost:16443"
	c.CertificateAuthority = caPath
	cfg.Clusters[cluster] = c

	auth := api.NewAuthInfo()
	auth.ClientCertificate = certPath
	auth.ClientKey = keyPath
	user := "system:node:" + n.Nodename()
	cfg.AuthInfos[user] = auth

	ctx := api.NewContext()
	ctx.AuthInfo = user
	ctx.Cluster = cluster
	cfg.Contexts["default"] = ctx
	cfg.CurrentContext = "default"

	return cfg
}

// GenerateKubeletConfiguration generates kubelet configuration.
// `params` must be validated beforehand.
func GenerateKubeletConfiguration(params cke.KubeletParams, nodeAddress string, running *kubeletv1beta1.KubeletConfiguration) *kubeletv1beta1.KubeletConfiguration {
	caPath := op.K8sPKIPath("ca.crt")
	tlsCertPath := op.K8sPKIPath("kubelet.crt")
	tlsKeyPath := op.K8sPKIPath("kubelet.key")

	// default values
	base := &kubeletv1beta1.KubeletConfiguration{
		ClusterDomain:         "cluster.local",
		RuntimeRequestTimeout: metav1.Duration{Duration: 15 * time.Minute},
		HealthzBindAddress:    "0.0.0.0",
		VolumePluginDir:       "/opt/volume/bin",
	}

	// This won't raise an error because of prior validation
	c, err := params.MergeConfig(base)
	if err != nil {
		panic(err)
	}

	// forced values
	c.TLSCertFile = tlsCertPath
	c.TLSPrivateKeyFile = tlsKeyPath
	c.Authentication = kubeletv1beta1.KubeletAuthentication{
		X509:    kubeletv1beta1.KubeletX509Authentication{ClientCAFile: caPath},
		Webhook: kubeletv1beta1.KubeletWebhookAuthentication{Enabled: pointer.BoolPtr(true)},
	}
	c.Authorization = kubeletv1beta1.KubeletAuthorization{Mode: kubeletv1beta1.KubeletAuthorizationModeWebhook}
	c.ClusterDNS = []string{nodeAddress}

	if running != nil {
		// Keep the running configurations while the node is running.
		// All these fields are described as:
		//     This field should not be updated without a full node
		//     reboot. It is safest to keep this value the same as the local config.
		//
		// ref:  https://pkg.go.dev/k8s.io/kubelet/config/v1beta1#KubeletConfiguration
		c.KubeletCgroups = running.KubeletCgroups
		c.SystemCgroups = running.SystemCgroups
		c.CgroupRoot = running.CgroupRoot
		c.CgroupsPerQOS = running.CgroupsPerQOS
		c.CgroupDriver = running.CgroupDriver
		c.CPUManagerPolicy = running.CPUManagerPolicy
		c.TopologyManagerPolicy = running.TopologyManagerPolicy
		c.QOSReserved = running.QOSReserved
		c.SystemReservedCgroup = running.SystemReservedCgroup
		c.KubeReservedCgroup = running.KubeReservedCgroup
	}
	return c
}
