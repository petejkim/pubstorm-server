var colors = ['pink', 'yellow'],
    i = 0;

var disco = function() {
  i = (i + 1) % 2;
  document.body.style.backgroundColor = colors[i];
};

window.setInterval(disco, 500);

disco();
