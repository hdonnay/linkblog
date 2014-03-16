linkblog is a place to dump links.

It's inspired by [Tony Finch's link log](http://dotat.at/:/feed.html).

Buliding
--------

	% make

You'll need the go toolchain and `zip` installed. To hack on the stylesheet,
you'll also need [sass](http://sass-lang.com/).

Notes
-----

Only uses sqlite right now, I'm open to changing that.

Admin functions will all be under `/admin/`, but only `add` exists right now.

The `-pretty` argument allows you to specify an alternative root/url:

	% linkblog -pretty='http://example.com/links'

You can combine this with the `-l` arguement to run behind a proxy:

	% linkblog -l='127.0.0.1:7990' -pretty='http://example.com/links'

An example nginx config for the above invocation:

	location /links {
		proxy_pass http://127.0.0.1:7990;
	}
	location /links/admin {
		auth_basic            "links";
		auth_basic_user_file  /path/to/htpasswd;
		proxy_pass http://127.0.0.1:7990;
	}

