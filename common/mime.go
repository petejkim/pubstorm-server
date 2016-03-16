package common

import "mime"

func init() {
	mime.AddExtensionType(".htm", "text/html")
	mime.AddExtensionType(".html", "text/html")
	mime.AddExtensionType(".xml", "application/xml")
	mime.AddExtensionType(".xht", "text/xhtml+xml")
	mime.AddExtensionType(".xhtml", "text/xhtml+xml")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".txt", "text/plain")
	mime.AddExtensionType(".text", "text/plain")
	mime.AddExtensionType(".md", "text/plain")
	mime.AddExtensionType(".markdown", "text/plain")
	mime.AddExtensionType(".textile", "text/plain")

	// images
	mime.AddExtensionType(".png", "image/png")
	mime.AddExtensionType(".jpg", "image/jpeg")
	mime.AddExtensionType(".jpeg", "image/jpeg")
	mime.AddExtensionType(".gif", "image/gif")
	mime.AddExtensionType(".svg", "image/svg+xml")
	mime.AddExtensionType(".ico", "image/vnd.microsoft.icon")
	mime.AddExtensionType(".webp", "image/webp")

	// fonts
	mime.AddExtensionType(".eot", "application/vnd.ms-fontobject")
	mime.AddExtensionType(".woff", "application/font-woff")
	mime.AddExtensionType(".woff2", "application/font-woff2")
	mime.AddExtensionType(".otf", "application/x-font-opentype")
	mime.AddExtensionType(".ttf", "application/x-font-truetype")

	// audio
	mime.AddExtensionType(".mp3", "audio/mpeg")
	mime.AddExtensionType(".flac", "audio/flac")
	mime.AddExtensionType(".aac", "audio/aac")
	mime.AddExtensionType(".ogg", "audio/ogg")
	mime.AddExtensionType(".f4a", "audio/mp4")
	mime.AddExtensionType(".f4b", "audio/mp4")

	// video
	mime.AddExtensionType(".mpeg", "video/mpeg")
	mime.AddExtensionType(".mpg", "video/mpeg")
	mime.AddExtensionType(".mp4", "video/mp4")
	mime.AddExtensionType(".mov", "video/quicktime")
	mime.AddExtensionType(".ogv", "audio/ogv")
	mime.AddExtensionType(".avi", "video/msvideo")
	mime.AddExtensionType(".webm", "video/webm")
	mime.AddExtensionType(".m3u8", "application/vnd.apple.mpegurl")
	mime.AddExtensionType(".flv", "video/x-flv")
	mime.AddExtensionType(".f4v", "video/mp4")
	mime.AddExtensionType(".f4p", "video/mp4")

	// misc
	mime.AddExtensionType(".swf", "application/x-shockwave-flash")
	mime.AddExtensionType(".jar", "application/java-archive")
}
