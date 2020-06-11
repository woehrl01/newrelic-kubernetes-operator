/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"reflect"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrv1 "github.com/newrelic/newrelic-kubernetes-operator/api/v1"
	"github.com/newrelic/newrelic-kubernetes-operator/interfaces"
)

// AlertsChannelReconciler reconciles a AlertsChannel object
type AlertsChannelReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	AlertClientFunc func(string, string) (interfaces.NewRelicAlertsClient, error)
	apiKey          string
	Alerts          interfaces.NewRelicAlertsClient
	ctx             context.Context
}

// +kubebuilder:rbac:groups=nr.k8s.newrelic.com,resources=alertschannel,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nr.k8s.newrelic.com,resources=alertschannel/status,verbs=get;update;patch

func (r *AlertsChannelReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	r.ctx = context.Background()
	_ = r.Log.WithValues("alertsChannel", req.NamespacedName)

	var alertsChannel nrv1.AlertsChannel
	err := r.Client.Get(r.ctx, req.NamespacedName, &alertsChannel)
	if err != nil {
		if strings.Contains(err.Error(), " not found") {
			r.Log.Info("AlertsChannel 'not found' after being deleted. This is expected and no cause for alarm", "error", err)
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to GET alertsChannel", "name", req.NamespacedName.String())
		return ctrl.Result{}, nil
	}
	r.Log.Info("Starting reconcile action")
	r.Log.Info("alertsChannel", "alertsChannel.Spec", alertsChannel.Spec, "alertsChannel.status.applied", alertsChannel.Status.AppliedSpec)

	r.apiKey = r.getAPIKeyOrSecret(alertsChannel)

	if r.apiKey == "" {
		return ctrl.Result{}, errors.New("api key is blank")
	}
	//initial alertsClient
	alertsClient, errAlertsClient := r.AlertClientFunc(r.apiKey, alertsChannel.Spec.Region)
	if errAlertsClient != nil {
		r.Log.Error(errAlertsClient, "Failed to create AlertsClient")
		return ctrl.Result{}, errAlertsClient
	}
	r.Alerts = alertsClient

	deleteFinalizer := "alertschannels.finalizers.nr.k8s.newrelic.com"

	//examine DeletionTimestamp to determine if object is under deletion
	if alertsChannel.DeletionTimestamp.IsZero() {
		if !containsString(alertsChannel.Finalizers, deleteFinalizer) {
			alertsChannel.Finalizers = append(alertsChannel.Finalizers, deleteFinalizer)
		}
	} else {
		err := r.deleteAlertsChannel(&alertsChannel, deleteFinalizer)
		if err != nil {
			r.Log.Error(err, "error deleting channel", "name", alertsChannel.Name)
			return ctrl.Result{}, err
		}
	}

	if reflect.DeepEqual(&alertsChannel.Spec, alertsChannel.Status.AppliedSpec) {
		return ctrl.Result{}, nil
	}

	r.Log.Info("Reconciling", "alertsChannel", alertsChannel.Name)

	r.checkForExistingAlertsChannel(&alertsChannel)

	if alertsChannel.Status.ChannelID != 0 {
		err := r.updateAlertsChannel(&alertsChannel)
		if err != nil {
			r.Log.Error(err, "error updating alertsChannel")
			return ctrl.Result{}, err
		}
	} else {
		err := r.createAlertsChannel(&alertsChannel)
		if err != nil {
			r.Log.Error(err, "Error creating alertsChannel")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

//SetupWithManager - Sets up Controller for AlertsChannel
func (r *AlertsChannelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nrv1.AlertsChannel{}).
		Complete(r)
}

func (r *AlertsChannelReconciler) getAPIKeyOrSecret(alertschannel nrv1.AlertsChannel) string {

	if alertschannel.Spec.APIKey != "" {
		return alertschannel.Spec.APIKey
	}
	if alertschannel.Spec.APIKeySecret != (nrv1.NewRelicAPIKeySecret{}) {
		key := types.NamespacedName{Namespace: alertschannel.Spec.APIKeySecret.Namespace, Name: alertschannel.Spec.APIKeySecret.Name}
		var apiKeySecret v1.Secret
		getErr := r.Client.Get(context.Background(), key, &apiKeySecret)
		if getErr != nil {
			r.Log.Error(getErr, "Failed to retrieve secret", "secret", apiKeySecret)
			return ""
		}
		return string(apiKeySecret.Data[alertschannel.Spec.APIKeySecret.KeyName])
	}
	return ""
}

func (r *AlertsChannelReconciler) deleteAlertsChannel(alertsChannel *nrv1.AlertsChannel, deleteFinalizer string) (err error) {

	r.Log.Info("Deleting AlertsChannel", "name", alertsChannel.Name, "ChannelName", alertsChannel.Spec.Name)

	if alertsChannel.Status.ChannelID != 0 {
		_, err = r.Alerts.DeleteChannel(alertsChannel.Status.ChannelID)
		if err != nil {
			r.Log.Error(err, "error deleting AlertsChannel", "name", alertsChannel.Name, "ChannelName", alertsChannel.Spec.Name)
		}

	}
	// Now remove finalizer
	alertsChannel.Finalizers = removeString(alertsChannel.Finalizers, deleteFinalizer)

	err = r.Client.Update(r.ctx, alertsChannel)
	if err != nil {
		r.Log.Error(err, "tried updating condition status", "name", alertsChannel.Name, "Namespace", alertsChannel.Namespace)
		return err
	}
	return nil
}

func (r *AlertsChannelReconciler) createAlertsChannel(alertsChannel *nrv1.AlertsChannel) error {
	r.Log.Info("Creating AlertsChannel", "name", alertsChannel.Name, "ChannelName", alertsChannel.Spec.Name)
	APIChannel := alertsChannel.Spec.APIChannel()
	createdCondition, err := r.Alerts.CreateChannel(APIChannel)
	if err != nil {
		r.Log.Error(err, "error creating AlertsChannel"+alertsChannel.Name)
		return err
	}
	alertsChannel.Status.ChannelID = createdCondition.ID
	alertsChannel.Status.AppliedSpec = &alertsChannel.Spec
	err = r.Client.Update(r.ctx, alertsChannel)
	if err != nil {
		r.Log.Error(err, "tried updating condition status", "name", alertsChannel.Name, "Namespace", alertsChannel.Namespace)
		return err
	}
	return nil
}

func (r *AlertsChannelReconciler) updateAlertsChannel(alertschannel *nrv1.AlertsChannel) error {
	return nil
}

func (r *AlertsChannelReconciler) checkForExistingAlertsChannel(alertsChannel *nrv1.AlertsChannel) {}
