var gulp = require('gulp');
var htmlmin = require('gulp-htmlmin');
var uglify = require('gulp-uglify');
var plumber = require('gulp-plumber');
var cleanCSS = require('gulp-clean-css');
var imagemin = require('gulp-imagemin');
var sitemap = require('gulp-sitemap');
var runSequence = require('run-sequence');
var merge = require('merge-stream');
var fs = require('fs');

var paths = {
  js: ['build/**/*.js'],
  css: ['build/**/*.css'],
  html: ['build/**/*.html', 'build/**/*.htm'],
  image: ['build/**/*.jpeg', 'build/**/*.jpg', 'build/**/*.png', 'build/**/*.gif', 'build/**/*.svg']
};

var jsErrorHandler = function(err) {
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

  console.log("[Error] " + errorMsg.join(':'));
};

var cssErrorHandler = function(err) {
  var errorMsg = [];
  if (err.errors.length > 0 || err.warnings.length > 0) {
    if (err.path) {
      var filename = err.path.split("build/", 2)[1];
      errorMsg.push(filename);
    }
    errorMsg = errorMsg.concat(err.errors);
    errorMsg = errorMsg.concat(err.warnings);
    console.log("[Error] " + errorMsg.join(':').replace(/[\r\n]/g, ''));
  }
};

var htmlErrorHandler = function(err) {
  var errorMsg = [];
  if (err.fileName) {
    var filename = err.fileName.split("build/", 2)[1];
    errorMsg.push(filename);
  }

  if (err.message) {
    errorMsg.push(err.message);
  }

  console.log("[Error] " + errorMsg.join(':').replace(/[\r\n]/g, ''));
};

var imageErrorHandler = function(err) {
  var filename;
  if (err.fileName) {
    filename = err.fileName.split("build/", 2)[1];
  }

  console.log("[Error] " + filename + ":Failed to optimize");
};

var sitemapErrorHandler = function(err) {
  if (err) {
    console.log("[Error] " + err);
  }
};

gulp.task('js', function() {
  return gulp.src(paths.js).pipe(plumber()).pipe(uglify().on('error', jsErrorHandler)).pipe(plumber.stop()).pipe(gulp.dest('build'));
});

gulp.task('css', function() {
  return gulp.src(paths.css).pipe(cleanCSS({ debug: true }, cssErrorHandler)).pipe(gulp.dest('build'));
});

gulp.task('html', function() {
  return gulp.src(paths.html).pipe(htmlmin({ collapseWhitespace: true, conservativeCollapse: true }).on('error', htmlErrorHandler)).pipe(gulp.dest('build'));
});

gulp.task('image', function() {
  return gulp.src(paths.image).pipe(imagemin().on('error', imageErrorHandler)).pipe(gulp.dest('build'));
});

gulp.task('sitemap', function() {
  if (process.env.DOMAIN_NAMES) {
    var xmlBody = ['<?xml version="1.0" encoding="UTF-8"?>', '<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">'];
    var domainNames = process.env.DOMAIN_NAMES.split(",");

    var tasks = domainNames.map(function(domainName) {
      var fileName = "sitemap-" + domainName.replace(/\./g, '-') + ".xml";
      var entry = ['<sitemap>',
        '<loc>http://' + domainName + '/sitemap/' + fileName + '</loc>',
        '<lastmod>' + new Date().toISOString() + '</lastmod>',
        '</sitemap>'
      ];

      xmlBody = xmlBody.concat(entry);
      return gulp.src(paths.html, { read: false }).pipe(sitemap({ siteUrl: domainName, fileName: fileName }).on('error', sitemapErrorHandler)).pipe(gulp.dest('build/sitemap'));
    });

    xmlBody.push('</sitemapindex>');
    fs.writeFile('build/sitemap.xml', xmlBody.join(''), sitemapErrorHandler);
    return merge(tasks);
  }
});

gulp.task('fix-sitemap-permission', function() {
  if (process.env.DOMAIN_NAMES) {
    var domainNames = process.env.DOMAIN_NAMES.split(",");

    domainNames.forEach(function(domainName) {
      var fileName = "sitemap-" + domainName.replace(/\./g, '-') + ".xml";

      fs.chmod('build/sitemap/' + fileName, 0777, sitemapErrorHandler);
    });
    fs.chmod('build/sitemap', 0777, sitemapErrorHandler);
    fs.chmod('build/sitemap.xml', 0777, sitemapErrorHandler);
  }
});

gulp.task('default', function() {
  // 'html' task should be running after other tasks since it does not continue if there is any error
  runSequence('js', 'css', 'image', 'sitemap', 'fix-sitemap-permission', 'html');
});
