package e2e

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var kubeClient *kubernetes.Clientset
var dynClient dynamic.Interface
var ctx context.Context
var fooGVR schema.GroupVersionResource

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Foo Controller E2E Suite")
}

// -------------------
// Top-level setup
// -------------------
var _ = BeforeSuite(func() {
	ctx = context.Background()
	fooGVR = schema.GroupVersionResource{
		Group:    "example.com",
		Version:  "v1",
		Resource: "foos",
	}

	// Try in-cluster first, fallback to kubeconfig
	cfg, err := rest.InClusterConfig()
	if err != nil {
		cfg, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	}
	Expect(err).NotTo(HaveOccurred())

	kubeClient, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	dynClient, err = dynamic.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
})

// -------------------
// Tests
// -------------------
var _ = Describe("Foo Controller", func() {
	It("should create a Deployment when Foo is created", func() {
		foo := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "example.com/v1",
				"kind":       "Foo",
				"metadata": map[string]interface{}{
					"name":      "test-foo",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": int64(1),
				},
			},
		}

		_, err := dynClient.Resource(fooGVR).Namespace("default").Create(ctx, foo, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Wait until the controller creates the Deployment
		Eventually(func() bool {
			dep, err := kubeClient.AppsV1().Deployments("default").Get(ctx, "test-foo-deployment", metav1.GetOptions{})
			if err != nil {
				return false
			}
			return dep.Status.ReadyReplicas == 1
		}, 120*time.Second, 2*time.Second).Should(BeTrue())
	})
})
