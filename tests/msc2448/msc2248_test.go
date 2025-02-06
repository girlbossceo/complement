package tests

import (
	"bytes"
	"image"
	"image/png"
	"net/url"
	"testing"

	"github.com/buckket/go-blurhash"
	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/helpers"
	"github.com/matrix-org/complement/internal/data"
	"github.com/matrix-org/complement/must"
	"github.com/tidwall/gjson"
)

var decodedImage image.Image

func getDecodedImageInTest(t *testing.T) image.Image {
	if decodedImage != nil {
		return decodedImage
	}
	t.Helper()
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
