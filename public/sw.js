const CACHE = 'notflix-v2';
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
    this._dbPromise = new Promise( ( resolve, reject ) => {
      const req = indexedDB.open( name, version );
      req.onupgradeneeded = ( e ) => upgrade( req.result, e.oldVersion );
      req.onsuccess = () => resolve( req.result );
      req.onerror = () => reject( req.error );
    } );
  }

  async _tx ( store, mode, fn ) {
    const db = await this._dbPromise;

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
    return this._tx(
      store, 'readonly', ( s ) => s.get( key )
    );
  }
  put ( store, value ) {
    return this._tx(
      store, 'readwrite', ( s ) => s.put( value )
    );
  }
  del ( store, key ) {
    return this._tx(
      store, 'readwrite', ( s ) => s.delete( key )
    );
  }
  getAll ( store ) {
    return this._tx(
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


self.addEventListener( 'install', ( e ) => {
  e.waitUntil(
    caches
      .open( CACHE )
      .then( ( c ) => c.addAll( SHELL ) )
  );

  self.skipWaiting();
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
    e.respondWith(
      caches
        .open( OFFLINE_CACHE )
        .then( ( c ) => c.match( e.request ) )
        .then( ( r ) => r || fetch( e.request ) )
    );
    return;
  }

  if ( url.pathname.startsWith( '/api/' ) || url.pathname.startsWith( '/images/' ) ) return;

  e.respondWith(
    caches
      .match( e.request )
      .then( ( r ) => r || fetch( e.request ) )
  );
} );


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
  const mapping = await db.get( 'bgfetch-map', reg.id );
  if ( !mapping ) return;

  const { videoParam } = mapping;
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
  } catch ( _ ) { }

  const dlRecord = await db.get( 'downloads', videoParam );
  if ( dlRecord ) {
    dlRecord.status = 'done';
    dlRecord.downloadedAt = Date.now();

    await db.put( 'downloads', dlRecord );
  }

  await broadcast( {
    type: 'download-complete',
    videoParam,
    record: dlRecord
  } );
  try {
    await e.updateUI( {
      title: `Downloaded: ${ dlRecord?.title ?? videoParam }`
    } );
  } catch ( _ ) { }
}

async function getFail ( e ) {
  const reg = e.registration;
  const mapping = await db.get( 'bgfetch-map', reg.id );
  if ( !mapping ) return;

  const { videoParam } = mapping;
  const dlRecord = await db.get( 'downloads', videoParam );
  if ( dlRecord ) {
    dlRecord.status = 'error';
    await db.put( 'downloads', dlRecord );
  }

  await broadcast( {
    type: 'download-error',
    videoParam,
    record: dlRecord
  } );
}

async function getStop ( e ) {
  const reg = e.registration;
  const mapping = await db.get( 'bgfetch-map', reg.id );
  if ( !mapping ) return;

  const { videoParam } = mapping;
  await db.del( 'downloads', videoParam );
  await db.del( 'bgfetch-map', reg.id );

  await broadcast( {
    type: 'download-abort',
    videoParam,
    record: null
  } );
}
