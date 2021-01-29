//
// Copyright (c) 2019
// Mainflux
//
// SPDX-License-Identifier: Apache-2.0
//

package re

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/mainflux/mainflux"
	"github.com/mainflux/mainflux/logger"
	"github.com/mainflux/mainflux/pkg/errors"
	SDK "github.com/mainflux/mainflux/pkg/sdk/go"
)

const (
	host = "http://localhost:9081"
)

var (
	// ErrMalformedEntity indicates malformed entity specification (e.g.
	// invalid username or password).
	ErrMalformedEntity = errors.New("malformed entity specification")

	// ErrUnauthorizedAccess indicates missing or invalid credentials provided
	// when accessing a protected resource.
	ErrUnauthorizedAccess = errors.New("missing or invalid credentials provided")

	// ErrNotFound indicates a non-existent entity request.
	ErrNotFound = errors.New("non-existent entity")

	// ErrKuiperServer indicates internal kuiper rules engine server error
	ErrKuiperServer = errors.New("kuiper internal server error")
)

type Info struct {
	Version       string `json:"version"`
	Os            string `json:"os"`
	UpTimeSeconds int    `json:"upTimeSeconds"`
}

// Service specifies an API that must be fullfiled by the domain service
// implementation, and all of its decorators (e.g. logging & metrics).
type Service interface {
	Info(ctx context.Context) (Info, error)
	CreateStream(ctx context.Context, token, name, topic, row string) (string, error)
	UpdateStream(ctx context.Context, token, name, topic, row string) (string, error)
	ListStreams(ctx context.Context, token string) ([]string, error)
	ViewStream(ctx context.Context, token, name string) (Stream, error)
	DeleteStream(ctx context.Context, token, name string) (string, error)

	CreateRule(ctx context.Context, token string, rule Rule) (string, error)
}

type reService struct {
	auth   mainflux.AuthServiceClient
	sdk    SDK.SDK
	logger logger.Logger
}

var _ Service = (*reService)(nil)

// New instantiates the re service implementation.
func New(auth mainflux.AuthServiceClient, sdk SDK.SDK, logger logger.Logger) Service {
	return &reService{
		auth:   auth,
		sdk:    sdk,
		logger: logger,
	}
}

func (re *reService) Info(_ context.Context) (Info, error) {
	res, err := http.Get(host)
	if err != nil {
		return Info{}, errors.Wrap(ErrKuiperServer, err)

	}
	defer res.Body.Close()

	var i Info
	err = json.NewDecoder(res.Body).Decode(&i)
	if err != nil {
		return Info{}, errors.Wrap(ErrKuiperServer, err)
	}

	return i, nil
}

func (re *reService) CreateStream(ctx context.Context, token, name, topic, row string) (string, error) {
	ui, err := re.auth.Identify(ctx, &mainflux.Token{Value: token})
	if err != nil {
		return "", ErrUnauthorizedAccess
	}
	_, err = re.sdk.Channel(topic, token)
	if err != nil {
		return "", ErrUnauthorizedAccess
	}

	name = prepend(ui.Id, name)
	sql := sql(name, topic, row)
	body, err := json.Marshal(map[string]string{"sql": sql})
	url := fmt.Sprintf("%s/%s", host, "streams")
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}

	result, err := result(res, "Create stream", http.StatusCreated)
	if err != nil {
		return "", err
	}

	return result, nil
}

func (re *reService) UpdateStream(ctx context.Context, token, name, topic, row string) (string, error) {
	ui, err := re.auth.Identify(ctx, &mainflux.Token{Value: token})
	if err != nil {
		return "", ErrUnauthorizedAccess
	}
	_, err = re.sdk.Channel(topic, token)
	if err != nil {
		return "", ErrUnauthorizedAccess
	}

	name = prepend(ui.Id, name)
	sql := sql(name, topic, row)
	body, err := json.Marshal(map[string]string{"sql": sql})
	if err != nil {
		return "", errors.Wrap(ErrMalformedEntity, err)
	}
	url := fmt.Sprintf("%s/%s/%s", host, "streams", name)
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}

	result, err := result(res, "Update stream", http.StatusOK)
	if err != nil {
		return "", err
	}

	return result, nil
}

func (re *reService) ListStreams(ctx context.Context, token string) ([]string, error) {
	ui, err := re.auth.Identify(ctx, &mainflux.Token{Value: token})
	if err != nil {
		return []string{}, ErrUnauthorizedAccess
	}

	var streams []string
	url := fmt.Sprintf("%s/%s", host, "streams")
	res, err := http.Get(url)
	if err != nil {
		return streams, errors.Wrap(ErrKuiperServer, err)
	}
	defer res.Body.Close()

	err = json.NewDecoder(res.Body).Decode(&streams)
	if err != nil {
		return streams, errors.Wrap(ErrMalformedEntity, err)
	}

	for i, value := range streams {
		streams[i] = remove(ui.Id, value)
	}

	return streams, nil
}

func (re *reService) ViewStream(ctx context.Context, token, name string) (Stream, error) {
	ui, err := re.auth.Identify(ctx, &mainflux.Token{Value: token})
	if err != nil {
		return Stream{}, ErrUnauthorizedAccess
	}

	var stream Stream
	name = prepend(ui.Id, name)
	url := fmt.Sprintf("%s/%s/%s", host, "streams", name)

	res, err := http.Get(url)
	if err != nil {
		return stream, errors.Wrap(ErrKuiperServer, err)
	}
	if res.StatusCode == http.StatusNotFound {
		return stream, errors.Wrap(ErrNotFound, err)
	}
	defer res.Body.Close()

	err = json.NewDecoder(res.Body).Decode(&stream)
	if err != nil {
		return stream, errors.Wrap(ErrMalformedEntity, err)
	}
	stream.Name = remove(ui.Id, stream.Name)

	return stream, nil
}

func (re *reService) DeleteStream(ctx context.Context, token, name string) (string, error) {
	ui, err := re.auth.Identify(ctx, &mainflux.Token{Value: token})
	if err != nil {
		return "", ErrUnauthorizedAccess
	}

	name = prepend(ui.Id, name)
	url := fmt.Sprintf("%s/%s/%s", host, "streams", name)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}

	result, err := result(res, "Delete stream", http.StatusOK)
	if err != nil {
		return "", err
	}

	return result, nil
}

func (re *reService) CreateRule(ctx context.Context, token string, rule Rule) (string, error) {
	ui, err := re.auth.Identify(ctx, &mainflux.Token{Value: token})
	if err != nil {
		return "", ErrUnauthorizedAccess
	}
	_, err = re.sdk.Channel(rule.Actions[0].Mainflux.Channel, token)
	if err != nil {
		return "", ErrUnauthorizedAccess
	}

	rule.ID = prepend(ui.Id, rule.ID)
	words := strings.Fields(rule.SQL)
	idx := indexOf("from", words) + 1
	words[idx] = prepend(ui.Id, words[idx])
	rule.SQL = strings.Join(words, " ")

	body, err := json.Marshal(rule)
	url := fmt.Sprintf("%s/%s", host, "rules")
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(ErrKuiperServer, err)
	}

	result, err := result(res, "Create rule", http.StatusCreated)
	if err != nil {
		return "", err
	}

	return result, nil
}

func sql(name, topic, row string) string {
	return fmt.Sprintf("create stream %s (%s) WITH (DATASOURCE = \"%s\" FORMAT = \"%s\" TYPE = \"%s\")", name, row, topic, FORMAT, TYPE)
}

func prefix(id string) string {
	return strings.ReplaceAll(id, "-", "") + "_"
}
func prepend(id, name string) string {
	return fmt.Sprintf("%s%s", prefix(id), name)
}

func remove(id, name string) string {
	return strings.Replace(name, prefix(id), "", 1)
}

func indexOf(element string, data []string) int {
	for k, v := range data {
		if element == v {
			return k
		}
	}
	return -1 //not found.
}

func result(res *http.Response, action string, status int) (string, error) {

	result := action + " successful."
	if res.StatusCode != status {
		defer res.Body.Close()
		reason, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return "", errors.Wrap(ErrKuiperServer, err)
		}
		result = action + " failed. Kuiper http status: " + strconv.Itoa(res.StatusCode) + ". " + string(reason)
	}
	return result, nil
}