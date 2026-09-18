# Vares

### Vares means Inheritor


this project is a simple online distillator for llm's.

we currently support SFT distillation method. [more about other methods](https://youtu.be/YsMd4F3jfyg?si=yogfr5JcmPr_VURS)


---

## how it works?

you serve your app that uses an llm normally.

there are 2 scenarios, you either self-hosted your app, or you are using a provider.

in either case, you can set the `Echo` component in front of your api, and it Echo's everything to the trainer.

the trainer, is listening to the Echo module, waiting for incomming data. it will consume them, create a batch of good, de-duplicated data, and train the student for 1 iteration.

the more you use the app, the stronger the student gets.