package tenantmanager

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"
)

type TenantManager struct {
	Hostname string
	BaseUrl  string
	Log      logrus.FieldLogger
}

type Request struct {
	AvgConcurrentShoppers  int    `json:"avgConcurrentShoppers"`
	FromTime               string `json:"fromTime"`
	HostName               string `json:"hostName"`
	ID                     int    `json:"id"`
	PeakConcurrentShoppers int    `json:"peakConcurrentShoppers"`
	ServiceName            string `json:"serviceName"`
	Status                 string `json:"status"`
	TenantKey              string `json:"tenantKey"`
	Tier                   string `json:"tier"`
	ToTime                 string `json:"toTime"`
}

type Subscription struct {
	MaxInstanceCount int     `json:"maxInstanceCount"`
	MinInstanceCount int     `json:"minInstanceCount"`
	ServiceName      string  `json:"serviceName"`
	Status           string  `json:"status"`
	TenantKey        string  `json:"tenantKey"`
	Tier             string  `json:"tier"`
	Url              string  `json:"url"`
	Request          Request `json:"request"`
}

type Tenant struct {
	Address      string `json:"address"`
	ContactName  string `json:"contactName"`
	Email        string `json:"email"`
	OrgName      string `json:"orgName"`
	Phone        string `json:"phone"`
	Status       string `json:"status"`
	TenantKey    string `json:"tenantKey"`
	TenantName   string `json:"tenantName"`
	HeadingText  string `json:"headingText"`
	HeadingColor string `json:"headingColor"`
	RibbonColor  string `json:"ribbonColor"`
}

type HTTPResponseError struct {
	Cause      error  `json:"-"`
	Detail     string `json:"detail"`
	StatusCode int    `json:"-"`
}

func (e *HTTPResponseError) Error() string {
	if e.Cause == nil {
		return e.Detail
	}
	return e.Detail + " : " + e.Cause.Error()
}

func (t Tenant) TextOutput() string {
	p := fmt.Sprintf(
		"Address: %s\nContactName : %s\nEmail: %s\nOrgnNme: %s\nPhone: %s\n",
		t.Address, t.ContactName, t.Email, t.OrgName, t.Phone)
	return p
}

func (t *TenantManager) GetSubscriptionByHostnameUrl() string {
	return fmt.Sprintf("%s/subscribe/host/%s", t.BaseUrl, t.Hostname)
}

func (t *TenantManager) GetTenantByTenantKeyUrl(tenantKey string) string {
	return fmt.Sprintf("%s/tenant/%s", t.BaseUrl, tenantKey)
}

// GetSubscriptionByHostname is exported ...
func (t *TenantManager) GetSubscriptionByHostname() (Subscription, *HTTPResponseError) {
	var subscription Subscription
	//Build The URL string
	url := t.GetSubscriptionByHostnameUrl()
	t.Log.Debug("In GetSubscriptionByHostname url=" + url)
	insecure := true

	// Trust the augmented cert pool in our client
	config := &tls.Config{
		InsecureSkipVerify: insecure,
	}
	tr := &http.Transport{TLSClientConfig: config}
	client := &http.Client{Transport: tr}

	//We make HTTP request using the Get function

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Log.Error("An error occurred creating request, please try again", err)
		return subscription, &HTTPResponseError{
			Cause:      err,
			Detail:     err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	resp, err := client.Do(req)
	t.Log.Debug("In GetSubscriptionByHostname response=")
	t.Log.Debug(resp)
	if err != nil {
		t.Log.Error("An error occurred fetching tenant, please try again", err)
		return subscription, &HTTPResponseError{
			Cause:      err,
			Detail:     err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	defer resp.Body.Close()

	t.Log.Debug("In GetSubscriptionByHostname response=" + url)
	if resp.StatusCode != 200 {
		return subscription, &HTTPResponseError{
			Detail:     resp.Status,
			StatusCode: resp.StatusCode,
		}
	}

	//Decode the data
	if err := json.NewDecoder(resp.Body).Decode(&subscription); err != nil {
		t.Log.Error("An error occurred decoding the response, please try again", err)
		return subscription, &HTTPResponseError{
			Cause:      err,
			Detail:     err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	//Invoke the text output function & return it with nil as the error value
	return subscription, nil
}

// GetTenant is exported ...
func (t *TenantManager) GetTenanByTenantKey(tenantKey string) (Tenant, *HTTPResponseError) {
	var tenant Tenant
	//Build The URL string
	url := t.GetTenantByTenantKeyUrl(tenantKey)
	insecure := true

	// Trust the augmented cert pool in our client
	config := &tls.Config{
		InsecureSkipVerify: insecure,
	}
	tr := &http.Transport{TLSClientConfig: config}
	client := &http.Client{Transport: tr}

	//We make HTTP request using the Get function

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Log.Error("An error occurred creating request, please try again", err)
		return tenant, &HTTPResponseError{
			Cause:      err,
			Detail:     err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Log.Error("An error occurred fetcing tenant, please try again", err)
		return tenant, &HTTPResponseError{
			Cause:      err,
			Detail:     err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return tenant, &HTTPResponseError{
			Detail:     resp.Status,
			StatusCode: resp.StatusCode,
		}
	}

	//Decode the data
	if err := json.NewDecoder(resp.Body).Decode(&tenant); err != nil {
		t.Log.Error("An error occurred decoding the response, please try again", err)
		return tenant, &HTTPResponseError{
			Cause:      err,
			Detail:     err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	return tenant, nil
}

// TenantEnabled is exported ...
func (t *TenantManager) TenantEnabled() (bool, *HTTPResponseError) {

	enabled := false

	subscription, err := t.GetSubscriptionByHostname()
	if err != nil {
		return enabled, err
	}

	tenant, err := t.GetTenanByTenantKey(subscription.TenantKey)
	if err != nil {
		return enabled, err
	}

	return tenant.Status == "Running", nil
}

// TenantEnabled is exported ...
func (t *TenantManager) GetTenant() (Tenant, *HTTPResponseError) {
	subscription, err := t.GetSubscriptionByHostname()
	if err != nil {
		return Tenant{}, err
	}

	tenant, err := t.GetTenanByTenantKey(subscription.TenantKey)
	if err != nil {
		return Tenant{}, err
	}

	tenant.RibbonColor = "orange"

	return tenant, nil
}
