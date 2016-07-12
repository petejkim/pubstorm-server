package deployer

import (
	"bytes"
	"io"
	"strings"
)

var WatermarkScript = `<!-- PubStorm --><script type="text/javascript">
(function(p,u,b,s,t,o,r,m) {
  s = u.createElement('div');
  s.style.position = 'fixed';
  s.style.display = 'block';
  s.style.bottom = '20px';
  s.style.right = '20px';
  s.style.opacity = 0.8;
  s.style.backgroundColor = '#1e4ca1';
  s.style.borderRadius = '10px';
  s.innerHTML = '<a href="https://www.pubstorm.com/" style="' +
    'display: block;' +
    'padding: 8px 16px;' +
    'text-decoration: none;' +
    'color: #febe10;' +
    'font-family: Helvetica,Arial,sans-serif;' +
    'font-size: 16px;' +
    '" target="_blank">Powered by PubStorm</a>';
  u.body.appendChild(s);

  o = s.style.opacity;
  p.setTimeout(function() {
    t = p.setInterval(function() {
      if (o <= 0.05) {
        clearInterval(t);
        s.style.display = 'none';
      }
      s.style.opacity = o;
      s.style.filter = 'alpha(opacity=' + o * 100 + ')';
      o -= o * 0.05;
    }, 100);
  }, b*1000);
}(window,document,30));
</script><!-- END PubStorm -->`

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
