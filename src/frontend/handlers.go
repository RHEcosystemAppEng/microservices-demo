// Copyright 2018 Google LLC
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

package main

import (
	"context"
	"fmt"
	"html/template"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	pb "github.com/GoogleCloudPlatform/microservices-demo/src/frontend/genproto"
	"github.com/GoogleCloudPlatform/microservices-demo/src/frontend/money"
	tenantManager "github.com/GoogleCloudPlatform/microservices-demo/src/frontend/tenantmanager"
)

type platformDetails struct {
	css      string
	provider string
}

var (
	isCymbalBrand = "true" == strings.ToLower(os.Getenv("CYMBAL_BRANDING"))
	templates     = template.Must(template.New("").
			Funcs(template.FuncMap{
			"renderMoney":        renderMoney,
			"renderCurrencyLogo": renderCurrencyLogo,
		}).ParseGlob("templates/*.html"))
	plat               platformDetails
	customThemeTmpl, _ = template.ParseFiles("templates/customtheme.css")
)

var validEnvs = []string{"local", "gcp", "azure", "aws", "onprem", "alibaba"}

func (fe *frontendServer) homeHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}

	log.WithField("currency", currentCurrency(r)).Info("home")
	currencies, err := fe.getCurrencies(r.Context())
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve currencies"), http.StatusInternalServerError)
		return
	}
	products, err := fe.getProducts(r.Context())
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve products"), http.StatusInternalServerError)
		return
	}
	cart, err := fe.getCart(r.Context(), sessionID(r))
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve cart"), http.StatusInternalServerError)
		return
	}

	type productView struct {
		Item  *pb.Product
		Price *pb.Money
	}
	ps := make([]productView, len(products))
	for i, p := range products {
		price, err := fe.convertCurrency(r.Context(), p.GetPriceUsd(), currentCurrency(r))
		if err != nil {
			renderHTTPError(log, r, w, errors.Wrapf(err, "failed to do currency conversion for product %s", p.GetId()), http.StatusInternalServerError)
			return
		}
		ps[i] = productView{p, price}
	}

	// Set ENV_PLATFORM (default to local if not set; use env var if set; otherwise detect GCP, which overrides env)_
	var env = os.Getenv("ENV_PLATFORM")
	// Only override from env variable if set + valid env
	if env == "" || stringinSlice(validEnvs, env) == false {
		fmt.Println("env platform is either empty or invalid")
		env = "local"
	}
	// Autodetect GCP
	addrs, err := net.LookupHost("metadata.google.internal.")
	if err == nil && len(addrs) >= 0 {
		log.Debugf("Detected Google metadata server: %v, setting ENV_PLATFORM to GCP.", addrs)
		env = "gcp"
	}

	log.Debugf("ENV_PLATFORM is: %s", env)
	plat = platformDetails{}
	plat.setPlatformDetails(strings.ToLower(env))

	if err := templates.ExecuteTemplate(w, "home", map[string]interface{}{
		"session_id":        sessionID(r),
		"request_id":        r.Context().Value(ctxKeyRequestID{}),
		"user_currency":     currentCurrency(r),
		"show_currency":     true,
		"currencies":        currencies,
		"products":          ps,
		"cart_size":         cartSize(cart),
		"banner_color":      os.Getenv("BANNER_COLOR"), // illustrates canary deployments
		"ad":                fe.chooseAd(r.Context(), []string{}, log),
		"platform_css":      plat.css,
		"platform_name":     plat.provider,
		"is_cymbal_brand":   isCymbalBrand,
		"deploymentDetails": deploymentDetailsMap,
	}); err != nil {
		log.Error(err)
	}
}

func (plat *platformDetails) setPlatformDetails(env string) {
	if env == "aws" {
		plat.provider = "AWS"
		plat.css = "aws-platform"
	} else if env == "onprem" {
		plat.provider = "On-Premises"
		plat.css = "onprem-platform"
	} else if env == "azure" {
		plat.provider = "Azure"
		plat.css = "azure-platform"
	} else if env == "gcp" {
		plat.provider = "Google Cloud"
		plat.css = "gcp-platform"
	} else if env == "alibaba" {
		plat.provider = "Alibaba Cloud"
		plat.css = "alibaba-platform"
	} else {
		plat.provider = "local"
		plat.css = "local"
	}
}

func (fe *frontendServer) productHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		renderHTTPError(log, r, w, errors.New("product id not specified"), http.StatusBadRequest)
		return
	}
	log.WithField("id", id).WithField("currency", currentCurrency(r)).
		Debug("serving product page")

	p, err := fe.getProduct(r.Context(), id)
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve product"), http.StatusInternalServerError)
		return
	}
	currencies, err := fe.getCurrencies(r.Context())
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve currencies"), http.StatusInternalServerError)
		return
	}

	cart, err := fe.getCart(r.Context(), sessionID(r))
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve cart"), http.StatusInternalServerError)
		return
	}

	price, err := fe.convertCurrency(r.Context(), p.GetPriceUsd(), currentCurrency(r))
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "failed to convert currency"), http.StatusInternalServerError)
		return
	}

	// ignores the error retrieving recommendations since it is not critical
	recommendations, err := fe.getRecommendations(r.Context(), sessionID(r), []string{id})
	if err != nil {
		log.WithField("error", err).Warn("failed to get product recommendations")
	}

	product := struct {
		Item  *pb.Product
		Price *pb.Money
	}{p, price}

	if err := templates.ExecuteTemplate(w, "product", map[string]interface{}{
		"session_id":        sessionID(r),
		"request_id":        r.Context().Value(ctxKeyRequestID{}),
		"ad":                fe.chooseAd(r.Context(), p.Categories, log),
		"user_currency":     currentCurrency(r),
		"show_currency":     true,
		"currencies":        currencies,
		"product":           product,
		"recommendations":   recommendations,
		"cart_size":         cartSize(cart),
		"platform_css":      plat.css,
		"platform_name":     plat.provider,
		"is_cymbal_brand":   isCymbalBrand,
		"deploymentDetails": deploymentDetailsMap,
	}); err != nil {
		log.Println(err)
	}
}

func (fe *frontendServer) addToCartHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}
	quantity, _ := strconv.ParseUint(r.FormValue("quantity"), 10, 32)
	productID := r.FormValue("product_id")
	if productID == "" || quantity == 0 {
		renderHTTPError(log, r, w, errors.New("invalid form input"), http.StatusBadRequest)
		return
	}
	log.WithField("product", productID).WithField("quantity", quantity).Debug("adding to cart")

	p, err := fe.getProduct(r.Context(), productID)
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve product"), http.StatusInternalServerError)
		return
	}

	if err := fe.insertCart(r.Context(), sessionID(r), p.GetId(), int32(quantity)); err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "failed to add to cart"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("location", "/cart")
	w.WriteHeader(http.StatusFound)
}

func (fe *frontendServer) GetHostname(r *http.Request) string {
	hostname := r.Host
	if os.Getenv("DEBUG_HOSTNAME") != "" {
		hostname = os.Getenv("DEBUG_HOSTNAME")
	}
	return hostname
}

func (fe *frontendServer) CustomThemeHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}

	tenantKey := fe.getCookieHandler(w, r, log)
	if tenantKey == "" {
		t := &tenantManager.TenantManager{
			Hostname: r.Host,
			BaseUrl:  fe.tenantAddr,
			Log:      log,
		}
		subscription, err := t.GetSubscriptionByHostname()
		if err != nil {
			renderHTTPError(log, r, w, errors.Wrap(err, "Error retrieving Tenant information"), http.StatusForbidden)
			return
		}
		tenantKey = subscription.TenantKey
		log.Println("Tenant Key:" + tenantKey)
		fe.setCookieHandler(w, r, log, tenantKey)
	}

	w.Header().Set("Content-Type", "text/css")

	tm := &tenantManager.TenantManager{
		Hostname:  r.Host,
		BaseUrl:   fe.tenantAddr,
		Log:       log,
		TenantKey: tenantKey,
	}

	style, e := tm.GetStyle()

	if e != nil {
		return
	}

	ribbonColorMap := make(map[string]int)
	ribbonColorMap["R"] = style.RibbonColor.R
	ribbonColorMap["G"] = style.RibbonColor.G
	ribbonColorMap["B"] = style.RibbonColor.B
	ribbonColorMap["A"] = style.RibbonColor.A

	headingColorMap := make(map[string]int)
	headingColorMap["R"] = style.HeadingColor.R
	headingColorMap["G"] = style.HeadingColor.G
	headingColorMap["B"] = style.HeadingColor.B
	headingColorMap["A"] = style.HeadingColor.A

	data := map[string]interface{}{
		"RibbonColor":  ribbonColorMap,
		"HeadingColor": headingColorMap,
		"HeadingText":  style.HeadingText,
	}

	if err := customThemeTmpl.Execute(w, data); err != nil {
		log.Println(err)
	}
}

func (fe *frontendServer) emptyCartHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}
	log.Debug("emptying cart")

	if err := fe.emptyCart(r.Context(), sessionID(r)); err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "failed to empty cart"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("location", "/")
	w.WriteHeader(http.StatusFound)
}

func (fe *frontendServer) viewCartHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}
	log.Debug("view user cart")
	currencies, err := fe.getCurrencies(r.Context())
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve currencies"), http.StatusInternalServerError)
		return
	}
	cart, err := fe.getCart(r.Context(), sessionID(r))
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve cart"), http.StatusInternalServerError)
		return
	}

	// ignores the error retrieving recommendations since it is not critical
	recommendations, err := fe.getRecommendations(r.Context(), sessionID(r), cartIDs(cart))
	if err != nil {
		log.WithField("error", err).Warn("failed to get product recommendations")
	}

	shippingCost, err := fe.getShippingQuote(r.Context(), cart, currentCurrency(r))
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "failed to get shipping quote"), http.StatusInternalServerError)
		return
	}

	type cartItemView struct {
		Item     *pb.Product
		Quantity int32
		Price    *pb.Money
	}
	items := make([]cartItemView, len(cart))
	totalPrice := pb.Money{CurrencyCode: currentCurrency(r)}
	for i, item := range cart {
		p, err := fe.getProduct(r.Context(), item.GetProductId())
		if err != nil {
			renderHTTPError(log, r, w, errors.Wrapf(err, "could not retrieve product #%s", item.GetProductId()), http.StatusInternalServerError)
			return
		}
		price, err := fe.convertCurrency(r.Context(), p.GetPriceUsd(), currentCurrency(r))
		if err != nil {
			renderHTTPError(log, r, w, errors.Wrapf(err, "could not convert currency for product #%s", item.GetProductId()), http.StatusInternalServerError)
			return
		}

		multPrice := money.MultiplySlow(*price, uint32(item.GetQuantity()))
		items[i] = cartItemView{
			Item:     p,
			Quantity: item.GetQuantity(),
			Price:    &multPrice}
		totalPrice = money.Must(money.Sum(totalPrice, multPrice))
	}
	totalPrice = money.Must(money.Sum(totalPrice, *shippingCost))
	year := time.Now().Year()

	if err := templates.ExecuteTemplate(w, "cart", map[string]interface{}{
		"session_id":        sessionID(r),
		"request_id":        r.Context().Value(ctxKeyRequestID{}),
		"user_currency":     currentCurrency(r),
		"currencies":        currencies,
		"recommendations":   recommendations,
		"cart_size":         cartSize(cart),
		"shipping_cost":     shippingCost,
		"show_currency":     true,
		"total_cost":        totalPrice,
		"items":             items,
		"expiration_years":  []int{year, year + 1, year + 2, year + 3, year + 4},
		"platform_css":      plat.css,
		"platform_name":     plat.provider,
		"is_cymbal_brand":   isCymbalBrand,
		"deploymentDetails": deploymentDetailsMap,
	}); err != nil {
		log.Println(err)
	}
}

func (fe *frontendServer) placeOrderHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}
	log.Debug("placing order")

	var (
		email         = r.FormValue("email")
		streetAddress = r.FormValue("street_address")
		zipCode, _    = strconv.ParseInt(r.FormValue("zip_code"), 10, 32)
		city          = r.FormValue("city")
		state         = r.FormValue("state")
		country       = r.FormValue("country")
		ccNumber      = r.FormValue("credit_card_number")
		ccMonth, _    = strconv.ParseInt(r.FormValue("credit_card_expiration_month"), 10, 32)
		ccYear, _     = strconv.ParseInt(r.FormValue("credit_card_expiration_year"), 10, 32)
		ccCVV, _      = strconv.ParseInt(r.FormValue("credit_card_cvv"), 10, 32)
	)

	order, err := pb.NewCheckoutServiceClient(fe.checkoutSvcConn).
		PlaceOrder(r.Context(), &pb.PlaceOrderRequest{
			Email: email,
			CreditCard: &pb.CreditCardInfo{
				CreditCardNumber:          ccNumber,
				CreditCardExpirationMonth: int32(ccMonth),
				CreditCardExpirationYear:  int32(ccYear),
				CreditCardCvv:             int32(ccCVV)},
			UserId:       sessionID(r),
			UserCurrency: currentCurrency(r),
			Address: &pb.Address{
				StreetAddress: streetAddress,
				City:          city,
				State:         state,
				ZipCode:       int32(zipCode),
				Country:       country},
		})
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "failed to complete the order"), http.StatusInternalServerError)
		return
	}
	log.WithField("order", order.GetOrder().GetOrderId()).Info("order placed")

	order.GetOrder().GetItems()
	recommendations, _ := fe.getRecommendations(r.Context(), sessionID(r), nil)

	totalPaid := *order.GetOrder().GetShippingCost()
	for _, v := range order.GetOrder().GetItems() {
		multPrice := money.MultiplySlow(*v.GetCost(), uint32(v.GetItem().GetQuantity()))
		totalPaid = money.Must(money.Sum(totalPaid, multPrice))
	}

	currencies, err := fe.getCurrencies(r.Context())
	if err != nil {
		renderHTTPError(log, r, w, errors.Wrap(err, "could not retrieve currencies"), http.StatusInternalServerError)
		return
	}

	if err := templates.ExecuteTemplate(w, "order", map[string]interface{}{
		"session_id":        sessionID(r),
		"request_id":        r.Context().Value(ctxKeyRequestID{}),
		"user_currency":     currentCurrency(r),
		"show_currency":     false,
		"currencies":        currencies,
		"order":             order.GetOrder(),
		"total_paid":        &totalPaid,
		"recommendations":   recommendations,
		"platform_css":      plat.css,
		"platform_name":     plat.provider,
		"is_cymbal_brand":   isCymbalBrand,
		"deploymentDetails": deploymentDetailsMap,
	}); err != nil {
		log.Println(err)
	}
}

func (fe *frontendServer) logoutHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	log.Debug("logging out")
	for _, c := range r.Cookies() {
		c.Expires = time.Now().Add(-time.Hour * 24 * 365)
		c.MaxAge = -1
		http.SetCookie(w, c)
	}
	w.Header().Set("Location", "/")
	w.WriteHeader(http.StatusFound)
}

func (fe *frontendServer) setCurrencyHandler(w http.ResponseWriter, r *http.Request) {
	log := r.Context().Value(ctxKeyLog{}).(logrus.FieldLogger)
	enabled, err := fe.tenantEnabled(w, r, log)
	if !enabled {
		renderHTTPError(log, r, w, errors.Wrap(err, "site is disabled"), http.StatusForbidden)
		return
	}
	cur := r.FormValue("currency_code")
	log.WithField("curr.new", cur).WithField("curr.old", currentCurrency(r)).
		Debug("setting currency")

	if cur != "" {
		http.SetCookie(w, &http.Cookie{
			Name:   cookieCurrency,
			Value:  cur,
			MaxAge: cookieMaxAge,
		})
	}
	referer := r.Header.Get("referer")
	if referer == "" {
		referer = "/"
	}
	w.Header().Set("Location", referer)
	w.WriteHeader(http.StatusFound)
}

// chooseAd queries for advertisements available and randomly chooses one, if
// available. It ignores the error retrieving the ad since it is not critical.
func (fe *frontendServer) chooseAd(ctx context.Context, ctxKeys []string, log logrus.FieldLogger) *pb.Ad {
	ads, err := fe.getAd(ctx, ctxKeys)
	if err != nil {
		log.WithField("error", err).Warn("failed to retrieve ads")
		return nil
	}
	return ads[rand.Intn(len(ads))]
}

func renderHTTPError(log logrus.FieldLogger, r *http.Request, w http.ResponseWriter, err error, code int) {
	log.WithField("error", err).Error("request error")
	errMsg := fmt.Sprintf("%+v", err)

	w.WriteHeader(code)

	if templateErr := templates.ExecuteTemplate(w, "error", map[string]interface{}{
		"session_id":        sessionID(r),
		"request_id":        r.Context().Value(ctxKeyRequestID{}),
		"error":             errMsg,
		"status_code":       code,
		"status":            http.StatusText(code),
		"is_cymbal_brand":   isCymbalBrand,
		"deploymentDetails": deploymentDetailsMap,
	}); templateErr != nil {
		log.Println(templateErr)
	}
}

func (fe *frontendServer) setCookieHandler(w http.ResponseWriter, r *http.Request, log logrus.FieldLogger, tenantKey string) {
	// Initialize a new cookie containing the tenantKey and some
	// non-default attributes.
	log.Debug("in setCookieHandler()")
	cookie := http.Cookie{
		Name:     "tenantKey",
		Value:    tenantKey,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	// Use the http.SetCookie() function to send the cookie to the client.
	// Behind the scenes this adds a `Set-Cookie` header to the response
	// containing the necessary cookie data.
	http.SetCookie(w, &cookie)

	log.Println("Tenant Key cooke set to " + tenantKey)
}

func (fe *frontendServer) getCookieHandler(w http.ResponseWriter, r *http.Request, log logrus.FieldLogger) string {
	// Retrieve the cookie from the request using its name (which in our case is
	// "exampleCookie"). If no matching cookie is found, this will return a
	// http.ErrNoCookie error. We check for this, and return a 400 Bad Request
	// response to the client.
	log.Debug("in getCookieHandler()")
	cookie, err := r.Cookie("tenantKey")
	if err != nil {
		switch {
		case errors.Is(err, http.ErrNoCookie):
			return ""
		default:
			log.Println(err)
			return ""
		}
	}

	// Echo out the cookie value in the response body.
	log.Debug("Cookie value retrieved from request: " + cookie.Value)
	return cookie.Value
}

func (fe *frontendServer) tenantEnabled(w http.ResponseWriter, r *http.Request, log logrus.FieldLogger) (bool, error) {
	log.Debug("verify: tenant is enabled")

	var err error
	tenantKey := fe.getCookieHandler(w, r, log)

	if tenantKey == "" {
		t := &tenantManager.TenantManager{
			Hostname: r.Host,
			BaseUrl:  fe.tenantAddr,
			Log:      log,
		}
		subscription, err := t.GetSubscriptionByHostname()
		if err != nil {
			return false, errors.New("Error retrieving Tenant information. Please contact the administrator.")
		}
		tenantKey = subscription.TenantKey
		log.Println("Tenant Key:" + tenantKey)
		fe.setCookieHandler(w, r, log, tenantKey)

	}

	tm := &tenantManager.TenantManager{
		Hostname:  r.Host,
		BaseUrl:   fe.tenantAddr,
		Log:       log,
		TenantKey: tenantKey,
	}

	enabled, err := tm.TenantEnabled()
	if err != nil {
		log.Println(err)
	}

	return enabled, errors.New("Site is disabled. Please contact the administrator.")

}

func currentCurrency(r *http.Request) string {
	c, _ := r.Cookie(cookieCurrency)
	if c != nil {
		return c.Value
	}
	return defaultCurrency
}

func sessionID(r *http.Request) string {
	v := r.Context().Value(ctxKeySessionID{})
	if v != nil {
		return v.(string)
	}
	return ""
}

func cartIDs(c []*pb.CartItem) []string {
	out := make([]string, len(c))
	for i, v := range c {
		out[i] = v.GetProductId()
	}
	return out
}

// get total # of items in cart
func cartSize(c []*pb.CartItem) int {
	cartSize := 0
	for _, item := range c {
		cartSize += int(item.GetQuantity())
	}
	return cartSize
}

func renderMoney(money pb.Money) string {
	currencyLogo := renderCurrencyLogo(money.GetCurrencyCode())
	return fmt.Sprintf("%s%d.%02d", currencyLogo, money.GetUnits(), money.GetNanos()/10000000)
}

func renderCurrencyLogo(currencyCode string) string {
	logos := map[string]string{
		"USD": "$",
		"CAD": "$",
		"JPY": "¥",
		"EUR": "€",
		"TRY": "₺",
		"GBP": "£",
	}

	logo := "$" //default
	if val, ok := logos[currencyCode]; ok {
		logo = val
	}
	return logo
}

func stringinSlice(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
