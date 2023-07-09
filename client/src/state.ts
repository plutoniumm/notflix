let user: User | null = null;

const random_names = [
  "Harry", "Hermione", "Ron", "Dumbledore", "Snape", "Voldemort", "Hagrid", "Sirius", "Lupin", "Bellatrix", "Draco", "Dobby", "Neville", "Fred", "George", "Ginny", "Luna", "Cho", "Cedric", "Tonks", "McGonagall", "Flitwick", "Sprout", "Trelawney", "Filch", "Moody", "Krum", "Fleur", "Percy", "Arthur", "Molly", "Bill", "Charlie", "James", "Lily", "Petunia", "Vernon", "Dudley", "Lucius", "Narcissa", "Crabbe", "Goyle", "Pansy", "Umbridge", "Kingsley", "Ollivander",
  "Tony", "Steve", "Natasha", "Bruce", "Clint", "Thor", "Loki", "Wanda", "Vision", "Pietro", "Sam", "Bucky", "Pepper", "Nick", "Maria", "Phil", "Fury", "Betty", "Jane"
];

const initUser = (): User => {
  const ls = localStorage.getItem("user");
  if (ls)
    return JSON.parse(ls);

  const name = random_names[Math.floor(Math.random() * random_names.length)];
  const image = `https://api.dicebear.com/6.x/bottts/svg?seed=${name}`;

  let temp = { name, image };
  localStorage.setItem("user", JSON.stringify(temp));

  return temp;
};