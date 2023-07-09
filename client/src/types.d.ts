type uuid = string;
type Emoji = string;

type Image = URL | Emoji;

interface User {
  name: string;
  image: Image;
};

interface Message {
  id: number;
  room: uuid;
  text: string;
  user: User;
};