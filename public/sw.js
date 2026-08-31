const CACHE = 'notflix-v7';
const OFFLINE_CACHE = 'notflix-offline-v1';
const SHELL = [
  '/',
  '/assets/notflix.js',
  '/assets/notflix.css',
  '/assets/global.css',
  '/assets/atomic.css',
  '/assets/icon.svg',
  '/assets/tight.svg',
];


class IDB {
  constructor ( name, version, upgrade ) {
    this.dbPromise = new Promise( ( resolve, reject ) => {
      const req = indexedDB.open( name, version );
      req.onupgradeneeded = ( e ) => upgrade( req.result, e.oldVersion );
      req.onsuccess = () => resolve( req.result );
      req.onerror = () => reject( req.error );
    } );
  }

  async tx ( store, mode, fn ) {
    const db = await this.dbPromise;

    return new Promise( ( resolve, reject ) => {
      const req = fn(
        db
          .transaction( store, mode )
          .objectStore( store )
      );

      req.onsuccess = () => resolve( req.result );
      req.onerror = () => reject( req.error );
    } );
  }

  get ( store, key ) {
    return this.tx(
      store, 'readonly', ( s ) => s.get( key )
    );
  }
  put ( store, value ) {
    return this.tx(
      store, 'readwrite', ( s ) => s.put( value )
    );
  }
  del ( store, key ) {
    return this.tx(
      store, 'readwrite', ( s ) => s.delete( key )
    );
  }
  getAll ( store ) {
    return this.tx(
      store, 'readonly', ( s ) => s.getAll()
    );
  }
  async has ( store, key ) {
    return ( await this.get( store, key ) ) !== undefined;
  }
}

const db = new IDB( 'notflix-offline', 1, ( D ) => {
  if ( !D.objectStoreNames.contains( 'downloads' ) )
    D.createObjectStore(
      'downloads', { keyPath: 'videoParam' }
    );

  if ( !D.objectStoreNames.contains( 'bgfetch-map' ) )
    D.createObjectStore(
      'bgfetch-map', { keyPath: 'bgFetchId' }
    );
} );


// No skipWaiting on install: taking over immediately reloads the page
// mid-playback. The swap waits for the client's SKIP_WAITING message.
self.addEventListener( 'install', ( e ) => {
  e.waitUntil( fill() );
} );

async function fill () {
  const c = await caches.open( CACHE );
  const done = await Promise.allSettled(
    SHELL.map( ( u ) => c.add( u ) )
  );

  const bad = SHELL.filter( ( _, i ) => done[ i ].status === 'rejected' );
  if ( bad.length ) console.warn( '[sw install] shell misses', bad );
}

self.addEventListener( 'message', ( e ) => {
  if ( e.data?.type === 'SKIP_WAITING' ) self.skipWaiting();
} );

self.addEventListener( 'activate', ( e ) => {
  e.waitUntil(
    caches.keys().then( ( keys ) =>
      Promise.all(
        keys
          .filter( ( k ) => k !== CACHE && k !== OFFLINE_CACHE )
          .map( ( k ) => caches.delete( k ) )
      )
    )
  );
  self.clients.claim();
} );


self.addEventListener( 'fetch', ( e ) => {
  const url = new URL( e.request.url );
  if ( e.request.method !== 'GET' ) return;

  if ( url.pathname.startsWith( '/video/' ) || url.pathname.startsWith( '/subs/' ) ) {
    e.respondWith( offline( e.request ) );

    return;
  }

  if ( url.pathname.startsWith( '/api/' ) ||
       url.pathname.startsWith( '/list/' ) ||
       url.pathname.startsWith( '/kv/' ) ||
       url.pathname.startsWith( '/images/' ) ) return;

  if ( e.request.mode === 'navigate' ) {
    e.respondWith(
      fetch( e.request )
        .then( ( response ) => {
          const clone = response.clone();
          caches.open( CACHE ).then( ( c ) => c.put( '/', clone ) );
          return response;
        } )
        .catch( () =>
          caches.open( CACHE ).then( ( c ) => c.match( '/' ) )
            .then( ( r ) => r || new Response( 'Offline', { status: 503 } ) )
        )
    );
    return;
  }

  e.respondWith(
    fetch( e.request )
      .then( ( response ) => {
        const clone = response.clone();
        caches.open( CACHE ).then( ( c ) => c.put( e.request, clone ) );
        return response;
      } )
      .catch( () =>
        caches.open( CACHE ).then( ( c ) => c.match( e.request ) )
          .then( ( r ) => r || new Response( 'Offline', { status: 503 } ) )
      )
  );
} );

async function offline ( req ) {
  const c = await caches.open( OFFLINE_CACHE );
  const hit = await c.match( req );

  if ( !hit ) return fetch( req );

  return carve( hit, req.headers.get( 'range' ) );
}

// The Cache API ignores Range and only ever hands back the whole stored
// response, so a seek would get a 200 and never move. Slice it here.
async function carve ( res, range ) {
  const m = /^bytes=(\d*)-(\d*)$/.exec( ( range || '' ).trim() );
  if ( !m || ( !m[ 1 ] && !m[ 2 ] ) ) return res;

  const body = await res.blob();
  const size = body.size;
  let start, end;

  if ( m[ 1 ] ) {
    start = +m[ 1 ];
    end = m[ 2 ] ? Math.min( +m[ 2 ], size - 1 ) : size - 1;
  } else {
    start = Math.max( size - +m[ 2 ], 0 );
    end = size - 1;
  }

  if ( start >= size || start > end )
    return new Response( null, {
      status: 416,
      headers: {
        'Content-Range': `bytes */${ size }`,
        'Accept-Ranges': 'bytes'
      }
    } );

  const head = new Headers( res.headers );
  head.set( 'Content-Range', `bytes ${ start }-${ end }/${ size }` );
  head.set( 'Content-Length', String( end - start + 1 ) );
  head.set( 'Accept-Ranges', 'bytes' );

  return new Response( body.slice( start, end + 1 ), {
    status: 206,
    statusText: 'Partial Content',
    headers: head
  } );
}


self.addEventListener(
  'backgroundfetchsuccess',
  ( e ) => e.waitUntil( getDone( e ) )
);
self.addEventListener(
  'backgroundfetchfail',
  ( e ) => e.waitUntil( getFail( e ) )
);
self.addEventListener(
  'backgroundfetchabort',
  ( e ) => e.waitUntil( getStop( e ) )
);

async function broadcast ( msg ) {
  const clients = await self.clients
    .matchAll( { includeUncontrolled: true } );

  for ( const client of clients )
    client.postMessage( msg );
}

async function getDone ( e ) {
  const reg = e.registration;
  const mapping = await db.get( 'bgfetch-map', reg.id ).catch( ( err ) => {
    console.error( '[sw getDone] mapping lookup failed', err );
    return null;
  } );
  if ( !mapping ) {
    console.warn( '[sw getDone] no mapping for', reg.id );
    return;
  }

  const { videoParam } = mapping;
  let cacheFailure = null;

  try {
    const records = await reg.matchAll();
    const cache = await caches.open( OFFLINE_CACHE );

    for ( const record of records ) {
      const response = await record.responseReady;
      await cache.put( record.request, response );
    }

    const subPath = videoParam.replace( /\.mp4$/i, '.vtt' );
    try {
      const subRes = await fetch( `/subs/${ subPath }` );
      if ( subRes.ok )
        await cache.put( `/subs/${ subPath }`, subRes );
    } catch ( err ) {
      console.warn( '[sw getDone] subtitle cache skipped', err );
    }
  } catch ( err ) {
    console.error( '[sw getDone] cache.put failed', err );
    cacheFailure = err?.message || String( err );
  }

  const dlRecord = await db.get( 'downloads', videoParam ).catch( () => null );
  if ( dlRecord ) {
    dlRecord.status = cacheFailure ? 'error' : 'done';
    if ( cacheFailure ) dlRecord.error = cacheFailure;
    else dlRecord.downloadedAt = Date.now();

    try {
      await db.put( 'downloads', dlRecord );
    } catch ( err ) {
      console.error( '[sw getDone] db.put failed', err );
    }
  }

  await broadcast( {
    type: cacheFailure ? 'download-error' : 'download-complete',
    videoParam,
    record: dlRecord,
    error: cacheFailure || undefined,
  } ).catch( ( err ) => console.warn( '[sw broadcast]', err ) );
  try {
    await e.updateUI( {
      title: cacheFailure
        ? `Download failed: ${ dlRecord?.title ?? videoParam }`
        : `Downloaded: ${ dlRecord?.title ?? videoParam }`
    } );
  } catch ( err ) {
    console.warn( '[sw updateUI]', err );
  }
}

async function getFail ( e ) {
  const reg = e.registration;
  const mapping = await db.get( 'bgfetch-map', reg.id ).catch( () => null );
  if ( !mapping ) return;

  const { videoParam } = mapping;
  let dlRecord = null;
  try {
    dlRecord = await db.get( 'downloads', videoParam );
    if ( dlRecord ) {
      dlRecord.status = 'error';
      await db.put( 'downloads', dlRecord );
    }
  } catch ( err ) {
    console.error( '[sw getFail] db op failed', err );
  }

  await broadcast( {
    type: 'download-error',
    videoParam,
    record: dlRecord
  } ).catch( ( err ) => console.warn( '[sw broadcast]', err ) );
}

async function getStop ( e ) {
  const reg = e.registration;
  const mapping = await db.get( 'bgfetch-map', reg.id ).catch( () => null );
  if ( !mapping ) return;

  const { videoParam } = mapping;
  try {
    await db.del( 'downloads', videoParam );
    await db.del( 'bgfetch-map', reg.id );
  } catch ( err ) {
    console.error( '[sw getStop] db op failed', err );
  }

  await broadcast( {
    type: 'download-abort',
    videoParam,
    record: null
  } ).catch( ( err ) => console.warn( '[sw broadcast]', err ) );
}
