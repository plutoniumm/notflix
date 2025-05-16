let player;
videojs( "my-video" ).ready( function () {
  player = this;

  let lastPlayed = localStorage.getItem( 'lastPlayed' );
  lastPlayed = JSON.parse( lastPlayed || "{}" );
  if ( lastPlayed[ video ] ) {
    player.currentTime( lastPlayed[ video ] );
    document.querySelector( 'video' )
  } else {
    player.currentTime( 0 );
    lastPlayed[ video ] = 0;
  }

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

  console.log( `Player is ready!`, player );
  addHotkeys( player );
} );

function addHotkeys ( player ) {
  document.addEventListener( 'keydown', ( event ) => {
    console.log( `[KEY] ${ event.which }` );

    if ( event.which === 39 ) { // +5s
      player.currentTime( player.currentTime() + 5 );
    } else if ( event.which === 37 ) { // -5s
      player.currentTime( player.currentTime() - 5 );
    } else if ( event.which === 32 ) { // play/pause
      player.paused() ? player.play() : player.pause();
    } else if ( event.which === 77 ) { // mute/unmute
      player.muted() ? player.muted( false ) : player.muted( true );
    } else if ( event.which === 70 ) { // fullscreen
      player.isFullscreen() ? player.exitFullscreen() : player.requestFullscreen();
    } else if ( event.which >= 48 && event.which <= 57 ) { // (0-9)0%
      let number = event.which - 48;
      player.currentTime( player.duration() * number * 0.1 );
    }
  } );
}


const listings = document.querySelector( '#listing' );
document.querySelector( '#enabler' ).addEventListener( 'click', () => {
  listings.classList.toggle( 'hidden' );
} );
if ( !video ) {
  listings.classList.remove( 'hidden' );
}

if ( video ) {
  player.addRemoteTextTrack( {
    kind: 'captions',
    src: `/subs/${ video }.vtt`,
    srclang: 'en',
    label: 'English',
    default: true
  } );
}