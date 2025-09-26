package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

var (
	masterURL     string
	kubeconfig    string
	finalizerName = "finalizer.foo.example.com"
)

type Controller struct {
	clientset     *kubernetes.Clientset
	dynamicClient dynamic.Interface
	fooInformer   cache.SharedIndexInformer
	workqueue     workqueue.RateLimitingInterface
}

func NewController(
	clientset *kubernetes.Clientset,
	dynamicClient dynamic.Interface,
	fooInformer cache.SharedIndexInformer) *Controller {

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	fooInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
				klog.Infof("Added: %s", key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(key)
				klog.Infof("Updated: %s", key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
				klog.Infof("Deleted: %s", key)
			}
		},
	})

	return &Controller{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		fooInformer:   fooInformer,
		workqueue:     queue,
	}
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting Foo controller")

	go c.fooInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.fooInformer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	klog.Info("Foo controller synced and ready")

	wait.Until(c.runWorker, time.Second, stopCh)
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	key, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	defer c.workqueue.Done(key)

	klog.Infof("Processing key: %s", key)

	err := c.syncHandler(key.(string))
	if err == nil {
		c.workqueue.Forget(key)
	} else if c.workqueue.NumRequeues(key) < 5 {
		klog.Errorf("Error syncing %s: %v", key, err)
		c.workqueue.AddRateLimited(key)
	} else {
		klog.Errorf("Dropping %s out of the queue: %v", key, err)
		c.workqueue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	obj, exists, err := c.fooInformer.GetIndexer().GetByKey(namespace + "/" + name)
	if err != nil {
		return err
	}

	if !exists {
		klog.Infof("Foo %s/%s no longer exists", namespace, name)
		return nil
	}

	foo := obj.(*unstructured.Unstructured)
	if foo.GetDeletionTimestamp() != nil {
		klog.Infof("Foo %s/%s is marked for deletion", namespace, name)
		return c.handleDeletion(foo)
	}

	// Ensure the finalizer is added
	if !containsString(foo.GetFinalizers(), finalizerName) {
		return c.addFinalizer(foo)
	}

	replicas, found, err := unstructured.NestedInt64(foo.Object, "spec", "replicas")
	if err != nil || !found {
		return err
	}

	klog.Infof("Reconciling Foo %s/%s with %d replicas", namespace, name, replicas)
	return c.reconcileDeployment(namespace, name, int32(replicas))
}

func (c *Controller) reconcileDeployment(namespace, name string, replicas int32) error {
	desiredDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-deployment",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}

	existingDeployment, err := c.clientset.AppsV1().Deployments(namespace).Get(context.Background(), desiredDeployment.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Creating deployment for Foo %s/%s", namespace, name)
			_, err := c.clientset.AppsV1().Deployments(namespace).Create(context.Background(), desiredDeployment, metav1.CreateOptions{})
			return err
		}
		return err
	}

	if !deploymentEqual(existingDeployment, desiredDeployment) {
		klog.Infof("Updating deployment for Foo %s/%s", namespace, name)
		_, err := c.clientset.AppsV1().Deployments(namespace).Update(context.Background(), desiredDeployment, metav1.UpdateOptions{})
		return err
	}

	klog.Infof("Deployment for Foo %s/%s is up-to-date", namespace, name)
	return nil
}

func (c *Controller) handleDeletion(foo *unstructured.Unstructured) error {
	namespace := foo.GetNamespace()
	name := foo.GetName()
	deploymentName := name + "-deployment"

	klog.Infof("Deleting deployment %s/%s", namespace, deploymentName)
	err := c.clientset.AppsV1().Deployments(namespace).Delete(context.Background(), deploymentName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// Remove the finalizer to allow the Foo resource to be deleted
	foo.SetFinalizers(removeString(foo.GetFinalizers(), finalizerName))
	_, err = c.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "example.com",
		Version:  "v1",
		Resource: "foos",
	}).Namespace(namespace).Update(context.Background(), foo, metav1.UpdateOptions{})
	return err
}

func (c *Controller) addFinalizer(foo *unstructured.Unstructured) error {
	finalizers := foo.GetFinalizers()
	finalizers = append(finalizers, finalizerName)
	foo.SetFinalizers(finalizers)

	namespace := foo.GetNamespace()
	name := foo.GetName()
	klog.Infof("Adding finalizer to Foo %s/%s", namespace, name)

	_, err := c.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "example.com",
		Version:  "v1",
		Resource: "foos",
	}).Namespace(namespace).Update(context.Background(), foo, metav1.UpdateOptions{})
	return err
}

func deploymentEqual(a, b *appsv1.Deployment) bool {
	return *a.Spec.Replicas == *b.Spec.Replicas &&
		a.Spec.Template.Spec.Containers[0].Image == b.Spec.Template.Spec.Containers[0].Image
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

func main() {
	klog.InitFlags(nil)
	flag.Set("v", "2") // Set the log level to 2 for more detailed logs
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating clientset: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating dynamic client: %v", err)
	}

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, time.Second*30, metav1.NamespaceAll, nil)
	fooInformer := factory.ForResource(schema.GroupVersionResource{
		Group:    "example.com",
		Version:  "v1",
		Resource: "foos",
	}).Informer()

	controller := NewController(clientset, dynamicClient, fooInformer)

	stopCh := make(chan struct{})
	defer close(stopCh)

	go controller.Run(stopCh)

	<-stopCh
}
