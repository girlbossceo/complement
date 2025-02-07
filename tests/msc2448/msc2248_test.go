package tests

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/url"
	"testing"

	"github.com/buckket/go-blurhash"
	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/b"
	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/federation"
	"github.com/matrix-org/complement/helpers"
	"github.com/matrix-org/complement/internal/data"
	"github.com/matrix-org/complement/must"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/tidwall/gjson"
)

var decodedImage image.Image

func getDecodedImageInTest(t *testing.T) image.Image {
	t.Helper()
	if decodedImage != nil {
		return decodedImage
	}
	decoded_image, err := png.Decode(bytes.NewReader(data.MatrixPng))
	must.NotError(t, "failed at decoding test img", err)
	decodedImage = decoded_image
	return decodedImage
}

func getImageBlurInTest(t *testing.T, x_comp int, y_comp int) string {
	t.Helper()
	encodedBlur, err := blurhash.Encode(x_comp, y_comp, getDecodedImageInTest(t))
	must.NotError(t, "failed encoding test img into blur", err)
	return encodedBlur

}

var defaultBlur *string

func getDefaultImageBlurInTest(t *testing.T) string {
	t.Helper()
	if defaultBlur != nil {
		return *defaultBlur
	}
	cute := getImageBlurInTest(t, 4, 3)
	defaultBlur = &cute
	return *defaultBlur
}

// see https://github.com/matrix-org/matrix-spec-proposals/blob/anoa/blurhash/proposals/2448-blurhash-for-media.md#calculating-a-blurhash-on-the-server,
// this checks if the server calculates a blurhash and then returns it.
func TestServerCalculatesBlurhashWhenRequestedTo(t *testing.T) {
	deployment := complement.Deploy(t, 1)
	defer deployment.Destroy(t)
	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{})
	wantContentType := "image/png"
	var js gjson.Result
	t.Run("Upload Image With Blurhash param", func(t *testing.T) {
		query := url.Values{}
		query.Set("xyz.amorgan.generate_blurhash", "true")
		query.Set("filename", "large.png")
		res := alice.MustDo(
			t, "POST", []string{"_matrix", "media", "v3", "upload"},
			client.WithRawBody(data.MatrixPng), client.WithContentType(wantContentType), client.WithQueries(query),
		)
		js = must.ParseJSON(t, res.Body)
		defer res.Body.Close()
	})
	var blur string
	t.Run("Blurhash Exists in response", func(t *testing.T) {
		blur = must.GetJSONFieldStr(t, js, "xyz.amorgan.blurhash")
	})

	t.Run("Blurhash Properly Generated", func(t *testing.T) {
		x_comp, y_comp, err := blurhash.Components(blur)
		must.NotError(t, "Blurhash is Undecodable", err)
		clientBlur := getImageBlurInTest(t, x_comp, y_comp)
		must.Equal(t, blur, clientBlur, "Encoded blurhash does not match same settings locally encoded blurhash")
	})
}

// see https://github.com/matrix-org/matrix-spec-proposals/blob/anoa/blurhash/proposals/2448-blurhash-for-media.md#mroommessage,
// this check if the server preserves blurhashes in messages
func TestServerPreservesBlurhashInMessage(t *testing.T) {
	deployment := complement.Deploy(t, 1)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{})
	bob := deployment.Register(t, "hs1", helpers.RegistrationOpts{})

	roomID := alice.MustCreateRoom(t, map[string]interface{}{
		"invite":    []string{bob.UserID},
		"is_direct": true,
	})
	bob.MustJoinRoom(t, roomID, []string{"hs1"})
	wantContentType := "image/png"

	mxcUri := alice.UploadContent(t, data.MatrixPng, "test.png", wantContentType)
	eventID := alice.SendEventSynced(t, roomID, b.Event{
		Type: "m.room.message",
		Content: map[string]interface{}{
			"msgtype": "m.image",
			"body":    "test.png",
			"url":     mxcUri,
			"info": map[string]string{
				"xyz.amorgan.blurhash": getDefaultImageBlurInTest(t),
			},
		},
	})

	bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHas(roomID, func(r gjson.Result) bool {
		if r.Get("event_id").Str != eventID {
			return false
		}
		must.Equal(t, r.Get("content.info.xyz\\.amorgan\\.blurhash").Str, getDefaultImageBlurInTest(t), "blurhash should be the same in the message")
		return true
	}))
}

// see https://github.com/matrix-org/matrix-spec-proposals/blob/anoa/blurhash/proposals/2448-blurhash-for-media.md#profile-endpoints,
// this checks if the server properly propagates and queries for the metadata in the avatar url
func TestServerReturnsBlurhashForProfilePicture(t *testing.T) {
	deployment := complement.Deploy(t, 2)
	srv := federation.NewServer(t, deployment,
		federation.HandleKeyRequests(),
	)
	srv.Listen()
	defer deployment.Destroy(t)
	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{})
	wantContentType := "image/png"

	mxcUri := alice.UploadContent(t, data.MatrixPng, "test.png", wantContentType)
	alice.MustDo(t, "PUT", []string{"_matrix", "client", "v3", "profile", alice.UserID, "avatar_url"},
		client.WithJSONBody(t, map[string]interface{}{
			"avatar_url":           mxcUri,
			"xyz.amorgan.blurhash": getDefaultImageBlurInTest(t),
		}))
	defaultImageBlur := getDefaultImageBlurInTest(t)
	t.Run("Make sure others can get profile info with blurhash", func(t *testing.T) {
		t.Run("Make sure another homeserver user can get pfp", func(t *testing.T) {
			t.Parallel()
			bob := deployment.Register(t, "hs1", helpers.RegistrationOpts{})
			resp := bob.MustDo(t, "GET", []string{"_matrix", "client", "v3", "profile", alice.UserID, "avatar_url"})
			js := must.ParseJSON(t, resp.Body)
			must.Equal(t, must.GetJSONFieldStr(t, js, "xyz\\.amorgan\\.blurhash"), defaultImageBlur, "blurhash is not equal to one set by client")
			must.Equal(t, must.GetJSONFieldStr(t, js, "avatar_url"), mxcUri, "Avatar URL is not equal to one set by client")
			defer resp.Body.Close()
		})
		t.Run("Make sure a user on another homeserver can get url", func(t *testing.T) {
			t.Parallel()
			bob := deployment.Register(t, "hs2", helpers.RegistrationOpts{})
			resp := bob.MustDo(t, "GET", []string{"_matrix", "client", "v3", "profile", alice.UserID, "avatar_url"})
			js := must.ParseJSON(t, resp.Body)
			must.Equal(t, must.GetJSONFieldStr(t, js, "xyz\\.amorgan\\.blurhash"), defaultImageBlur, "blurhash is not equal to one set by client")
			must.Equal(t, must.GetJSONFieldStr(t, js, "avatar_url"), mxcUri, "Avatar URL is not equal to one set by client")
			defer resp.Body.Close()
		})
		t.Run("Make sure another homeserver can get blurhash with profile", func(t *testing.T) {
			t.Parallel()
			origin := spec.ServerName(srv.ServerName())
			fedRequest := fclient.NewFederationRequest("GET", origin, "hs1", "/_matrix/federation/v1/query/profile"+
				"?user_id="+alice.UserID)
			fedResponse, err := srv.DoFederationRequest(context.Background(), t, deployment, fedRequest)
			must.NotError(t, "failed to GET /profile", err)
			js := must.ParseJSON(t, fedResponse.Body)
			defer fedResponse.Body.Close()
			hash := must.GetJSONFieldStr(t, js, "xyz\\.amorgan\\.blurhash")
			must.Equal(t, hash, defaultImageBlur, "blurhash is not correct")
		})
	})
}
