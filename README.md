vanishling
--

Simple TTL based file hosting service.

Upload
--
Supports POST/PUT:

```fish
$ for f in (ls -d ~/Music/Collection1/**); curl -H 'x-ttl: 1m' -v -F file=@$f http://localhost:8080 ; end
```

HTTP/1.1 PUT:
```fish
$ for f in (ls -d ~/Music/Collection1/**); curl --http1.1 -X PUT -vv -H 'x-ttl: 10m1s' -k -F file=@$f https://abcd.ts.r.appspot.com/ ; end
```

Download and play music
--
The X-file-id header is the content hash (highway hash) digest returned by PUT/POST:
```fish
$ curl -o/dev/stdout -H 'X-file-id: e8b03fa35367aad7359397c13cbfad98093fc267e647f267cb27c05fdbcc6010' https://abcd.ts.r.appspot.com/ | cvlc -
```
