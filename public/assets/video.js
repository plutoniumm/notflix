videojs.Vhs.GOAL_BUFFER_LENGTH = 20;
videojs.Vhs.MAX_GOAL_BUFFER_LENGTH = 3000;
let player = videojs( 'my-video', {
  controls: true,
  html5: { hls: { overrideNative: true } },
  preload: true,

  plugins: {
    // hotkeys: {
    //   volumeStep: 0.1,
    //   seekStep: 5,
    // },
  },
} );

const listings = document.querySelector( '#listing' );
document.querySelector( '#enabler' ).addEventListener( 'click', () => {
  listings.classList.toggle( 'hidden' );
} );

let lastPlayed = localStorage.getItem( 'lastPlayed' );
lastPlayed = JSON.parse( lastPlayed || "{}" );
if ( lastPlayed[ video ] ) {
  player.currentTime( lastPlayed[ video ] );
  document.querySelector( 'video' )
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

if ( video ) {
  player.addRemoteTextTrack( {
    kind: 'captions',
    src: `/subs/${ video }.vtt`,
    srclang: 'en',
    label: 'English',
    default: true
  } );
}

// adding keyboard shortcuts
// document.addEventListener( 'keydown', ( e ) => {
//   if ( e.key === 'ArrowRight' ) {
//     player.currentTime( player.currentTime() + 10 );
//     // if numbers then jump to that percentage
//   } else if ( e.key >= 0 && e.key <= 9 ) {
//     player.currentTime( player.duration() * ( e.key / 10 ) );
//     // if e go to -5 seconds
//   } else if ( e.key === 'e' ) {
//     // player.currentTime( player.currentTime() - 5 );
//     player.currentTime( Math.max( player.currentTime() - 5, player.duration() * 0.99 ) );
//   } else if ( e.key === 'ArrowLeft' ) {
//     player.currentTime( player.currentTime() - 10 );
//   } else if ( e.key === ' ' ) {
//   };
// } );

console.log( "LOADED VIDEO.JS.M" );