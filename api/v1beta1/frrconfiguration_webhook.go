// SPDX-License-Identifier:Apache-2.0

package v1beta1

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	Logger        log.Logger
	WebhookClient client.Reader
	Validate      func(resources ...client.ObjectList) error
	Namespace     string
)

func (frrConfig *FRRConfiguration) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(frrConfig).
		Complete()
}

//+kubebuilder:webhook:verbs=create;update,path=/validate-frrk8s-metallb-io-v1beta1-frrconfiguration,mutating=false,failurePolicy=fail,groups=frrk8s.metallb.io,resources=frrconfigurations,versions=v1beta1,name=frrconfigurationsvalidationwebhook.metallb.io,sideEffects=None,admissionReviewVersions=v1

var _ webhook.Validator = &FRRConfiguration{}

type nodeAndConfigs struct {
	name   string
	labels map[string]string
	cfgs   *FRRConfigurationList
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for FRRConfiguration.
func (frrConfig *FRRConfiguration) ValidateCreate() error {
	level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "create", "name", frrConfig.Name, "namespace", frrConfig.Namespace)
	defer level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "end create", "name", frrConfig.Name, "namespace", frrConfig.Namespace)

	return validateConfig(frrConfig)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for FRRConfiguration.
func (frrConfig *FRRConfiguration) ValidateUpdate(old runtime.Object) error {
	level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "update", "name", frrConfig.Name, "namespace", frrConfig.Namespace)
	defer level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "end update", "name", frrConfig.Name, "namespace", frrConfig.Namespace)

	return validateConfig(frrConfig)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for FRRConfiguration.
func (frrConfig *FRRConfiguration) ValidateDelete() error {
	return nil
}

func validateConfig(frrConfig *FRRConfiguration) error {
	selector, err := metav1.LabelSelectorAsSelector(&frrConfig.Spec.NodeSelector)
	if err != nil {
		return errors.Wrapf(err, "resource contains an invalid NodeSelector")
	}

	existingNodes, err := getNodes()
	if err != nil {
		return err
	}

	existingFRRConfigurations, err := getFRRConfigurations()
	if err != nil {
		return err
	}

	matchingNodes := []nodeAndConfigs{}
	for _, n := range existingNodes {
		if selector.Matches(labels.Set(n.Labels)) {
			matchingNodes = append(matchingNodes, nodeAndConfigs{
				name:   n.Name,
				labels: n.Labels,
				cfgs:   &FRRConfigurationList{},
			})
		}
	}

	for _, n := range matchingNodes {
		for _, cfg := range existingFRRConfigurations.Items {
			nodeSelector := cfg.Spec.NodeSelector
			selector, err := metav1.LabelSelectorAsSelector(&nodeSelector)
			if err != nil {
				// shouldn't happen as it would have been denied earlier, just in case.
				continue
			}

			if cfg.Name == frrConfig.Name {
				// shouldn't happen for creates as it would be considered an update, and in any case
				// we add the updated one at the end because for updates we don't want the old and updated resources
				// to be considered together.
				continue
			}

			if selector.Matches(labels.Set(n.labels)) {
				n.cfgs.Items = append(n.cfgs.Items, *cfg.DeepCopy())
			}
		}
		n.cfgs.Items = append(n.cfgs.Items, *frrConfig.DeepCopy())
	}

	for _, n := range matchingNodes {
		err := Validate(n.cfgs)
		if err != nil {
			return errors.Wrapf(err, "resource is invalid for node %s", n.name)
		}
	}

	return nil
}

var getFRRConfigurations = func() (*FRRConfigurationList, error) {
	frrConfigurationsList := &FRRConfigurationList{}
	err := WebhookClient.List(context.Background(), frrConfigurationsList, &client.ListOptions{Namespace: Namespace})
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get existing FRRConfiguration objects")
	}
	return frrConfigurationsList, nil
}

var getNodes = func() ([]corev1.Node, error) {
	nodesList := &corev1.NodeList{}
	err := WebhookClient.List(context.Background(), nodesList)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get existing Node objects")
	}
	return nodesList.Items, nil
}
