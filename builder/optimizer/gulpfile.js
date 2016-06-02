var gulp = require('gulp');                                                                                                                                                                                                                                                                                         [14/598]
var htmlmin = require('gulp-htmlmin');
var uglify = require('gulp-uglify');
var plumber = require('gulp-plumber');
var cleanCSS = require('gulp-clean-css');
var imagemin = require('gulp-imagemin');
var runSequence = require('run-sequence');

var paths = {
  js: ['build/**/*.js'],
  css: ['build/**/*.css'],
  html: ['build/**/*.html'],
  image: ['build/**/*.jpeg','build/**/*.jpg','build/**/*.png','build/**/*.gif','build/**/*.svg']
};

jsErrorHandler = function(err) {
  var errorMsg = [];
  if (err.fileName) {
    var filename = err.fileName.split("build/", 2)[1];
    errorMsg.push(filename);
  }

  if (err.lineNumber) {
    errorMsg.push(err.lineNumber);
  }

  if (err.message) {
    var error = err.message.split(": ", 2)[1];
    errorMsg.push(error);
  }

  console.log("[Error] "+ errorMsg.join(':'));
}

cssErrorHandler = function(err) {
  var errorMsg = [];
  if (err.errors.length > 0 || err.warnings.length > 0) {
    if (err.path) {
      var filename = err.path.split("build/", 2)[1];
      errorMsg.push(filename);
    }
    errorMsg = errorMsg.concat(err.errors);
    errorMsg = errorMsg.concat(err.warnings);
    console.log("[Error] "+ errorMsg.join(':').replace(/[\r\n]/g, ''));
  }
}

htmlErrorHandler = function(err) {
  var errorMsg = [];
  if (err.fileName) {
    var filename = err.fileName.split("build/", 2)[1];
    errorMsg.push(filename);
  }

  if (err.message) {
    errorMsg.push(err.message);
  }

  console.log("[Error] "+ errorMsg.join(':').replace(/[\r\n]/g, ''));
}

imageErrorHandler = function(err) {
  var filename;
  if (err.fileName) {
    filename = err.fileName.split("build/", 2)[1];
  }

  console.log("[Error] " + filename + ":Failed to optimize");
}

gulp.task('js', function() {
  return gulp.src(paths.js).pipe(plumber()).pipe(uglify().on('error', jsErrorHandler)).pipe(plumber.stop()).pipe(gulp.dest('build'));
});

gulp.task('css', function() {
  return gulp.src(paths.css).pipe(cleanCSS({debug: true}, cssErrorHandler)).pipe(gulp.dest('build'));
});

gulp.task('html', function() {
  return gulp.src(paths.html).pipe(htmlmin({collapseWhitespace: true}).on('error', htmlErrorHandler)).pipe(gulp.dest('build'));
});

gulp.task('image', function() {
  return gulp.src(paths.image).pipe(imagemin().on('error', imageErrorHandler)).pipe(gulp.dest('build'));
});

gulp.task('default', function() {
  runSequence('js', 'css', 'image', 'html')
})
