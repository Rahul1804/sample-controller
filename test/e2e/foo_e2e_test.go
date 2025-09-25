package e2e

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	unstructuredv1 "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
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
		foo := &unstructuredv1.Unstructured{
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

		Eventually(func() bool {
			dep, err := kubeClient.AppsV1().Deployments("default").Get(ctx, "test-foo", metav1.GetOptions{})
			return err == nil && dep.Status.Replicas == 1
		}, 15*time.Second, 500*time.Millisecond).Should(BeTrue())
	})
})
