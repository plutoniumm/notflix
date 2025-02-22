videojs.Vhs.GOAL_BUFFER_LENGTH = 20;
videojs.Vhs.MAX_GOAL_BUFFER_LENGTH = 3000;
let player = videojs( 'my-video', {
  controls: true,
  html5: { hls: { overrideNative: true } },
  preload: true
} );

const listings = document.querySelector( '#listing' );
document.querySelector( '#enabler' ).addEventListener( 'click', () => {
  listings.classList.toggle( 'hidden' );
} );

let lastPlayed = localStorage.getItem( 'lastPlayed' );
lastPlayed = JSON.parse( lastPlayed || "{}" );
if ( lastPlayed[ video ] ) {
  player.currentTime( lastPlayed[ video ] );
  document.querySelector( 'video' ).removeAttribute( 'poster' );
  // document.querySelector( '.vjs-poster' ).remove();
} else {
  player.currentTime( 0 );
  lastPlayed[ video ] = 0;
}

// buffer patch, every second check if video is progressed or not
// if not, seek to current time
let lastTime = 0;
setInterval( () => {
  lastPlayed[ video ] = player.currentTime();
  localStorage.setItem( 'lastPlayed', JSON.stringify( lastPlayed ) );

  if ( player.paused() ) return;
  if ( player.currentTime() === lastTime ) {
    player.currentTime( player.currentTime() );
  };

  lastTime = player.currentTime();
}, 2000 );

player.addRemoteTextTrack( {
  kind: 'captions',
  src: `/subs/${ video }.vtt`,
  srclang: 'en',
  label: 'English',
  default: true
} );