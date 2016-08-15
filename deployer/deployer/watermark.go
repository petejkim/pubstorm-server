package deployer

import (
	"bytes"
	"io"
	"strings"
)

var WatermarkScript = `<!----><script type="text/javascript">(function(p,u,b,s,t,o,r,m) {
o=' !important;';s=u.createElement('div');s.innerHTML='<a style="'+
 ('position:fixed|display:block|bottom:0|left:auto|right:20px;|opacity:1|visibility:visible|background:#fff|'+
 'border-radius:3px 2px 0 0|transition:opacity .3s|margin:0|padding: 3px 5px|transform:none|float:none|z-index:999999|'+
 'font-family:Helvetica,Arial,sans-serif|color:#000|font-size:10px|font-weight:normal|border:none|outline:none|'+
 'box-shadow:0 1px 2px rgba(0,0,0,.3)|text-decoration:none|font-style:normal|line-height:1|vertical-align:middle').split('|').join(o)+
 '" href="https://www.pubstorm.com/?utm_source=pubstorm&utm_medium=watermark&utm_campaign=watermark" target="_blank">'+
 'Powered by <span style="font-weight:bold !important">PubStorm</span></a>';
u.body.appendChild(t=s.children[0]);
}(window,document));
</script>`

// TODO We should not read in the entire body of the io.Reader - it could be a
// very huge file that will consume lots of memory unnecessarily.
func injectWatermark(in io.Reader) (io.Reader, error) {
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, in); err != nil {
		return nil, err
	}

	s := buf.String()
	idx := strings.LastIndex(s, "</body>")
	if idx == -1 {
		return buf, nil
	}

	modified := s[:idx] + WatermarkScript + s[idx:]
	return bytes.NewBufferString(modified), nil
}
